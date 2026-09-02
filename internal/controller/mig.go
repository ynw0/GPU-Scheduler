package controller

import (
	"encoding/json"
	"net/http"
	"strings"

	"gpu-scheduler/internal/domain"
	"gpu-scheduler/internal/mig"
)

func (s *Server) listMIGLayouts(w http.ResponseWriter, r *http.Request) {
	items, err := s.store.ListMIGLayouts(r.Context())
	if err != nil {
		writeProblem(w, 500, "List MIG layouts failed", err.Error())
		return
	}
	writeJSON(w, 200, items)
}

func (s *Server) applyMIGLayout(w http.ResponseWriter, r *http.Request) {
	gpuUUID := r.PathValue("gpuUuid")
	var body struct {
		Enabled  bool     `json:"enabled"`
		Profiles []string `json:"profiles"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeProblem(w, 422, "Invalid request", err.Error())
		return
	}
	if body.Profiles == nil {
		body.Profiles = []string{}
	}
	if !body.Enabled {
		body.Profiles = []string{}
	}
	topology, err := s.liveTopology(r.Context())
	if err != nil {
		writeProblem(w, 503, "Topology unavailable", err.Error())
		return
	}
	var gpu domain.GPU
	var children []string
	found := false
	for _, item := range topology.GPUs {
		if item.UUID == gpuUUID && item.ParentUUID == "" {
			gpu = item
			found = true
		}
		if item.ParentUUID == gpuUUID {
			children = append(children, item.UUID)
		}
	}
	if !found {
		writeProblem(w, 404, "GPU not found", gpuUUID)
		return
	}
	if body.Enabled {
		if _, err := mig.Evaluate(mig.CatalogForMemory(gpu.MemoryBytes), body.Profiles); err != nil {
			writeProblem(w, 422, "Invalid MIG layout", err.Error())
			return
		}
		if len(body.Profiles) == 0 {
			writeProblem(w, 422, "Invalid MIG layout", "开启 MIG 时至少选择一个档位")
			return
		}
	}
	blocked := map[string]struct{}{gpuUUID: {}}
	for _, uuid := range children {
		blocked[uuid] = struct{}{}
	}
	allocations, err := s.store.ListActiveAllocations(r.Context())
	if err != nil {
		writeProblem(w, 500, "Allocations unavailable", err.Error())
		return
	}
	for _, allocation := range allocations {
		if allocation.ResourceType != "gpu" {
			continue
		}
		if _, busy := blocked[allocation.ResourceID]; busy {
			writeProblem(w, 409, "GPU busy", "该卡仍有受管任务占用，切分前必须结束任务")
			return
		}
	}
	claims, err := s.agent.Claims(r.Context())
	if err != nil {
		writeProblem(w, 503, "Claims unavailable", err.Error())
		return
	}
	for _, claim := range claims {
		if !strings.EqualFold(claim.State, "running") && !strings.EqualFold(claim.State, "created") && !strings.EqualFold(claim.State, "paused") {
			continue
		}
		for _, uuid := range append(claim.GPUUUIDs, claim.TrainingGPUUUIDs...) {
			if _, busy := blocked[uuid]; busy {
				writeProblem(w, 409, "GPU busy", "该卡仍有容器占用，切分前必须停干净")
				return
			}
		}
	}
	if err := s.agent.ApplyMIG(r.Context(), gpuUUID, gpu.MemoryBytes, body.Profiles); err != nil {
		writeProblem(w, 503, "Apply MIG failed", err.Error())
		return
	}
	layout := domain.MIGLayout{GPUUUID: gpuUUID, Enabled: body.Enabled, Profiles: body.Profiles}
	if err := s.store.UpsertMIGLayout(r.Context(), layout); err != nil {
		writeProblem(w, 500, "Save layout failed", err.Error())
		return
	}
	_ = s.store.InsertAudit(r.Context(), identityFrom(r.Context()).UserID, "gpu.mig", "gpu", gpuUUID, mig.FormatSlices(body.Profiles))
	writeJSON(w, 200, layout)
}
