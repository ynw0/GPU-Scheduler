package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"sort"
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
	gpu, children, ok := findPhysicalGPU(topology, gpuUUID)
	if !ok {
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
	busy, err := s.gpuUUIDsBusy(r.Context(), append([]string{gpuUUID}, children...))
	if err != nil {
		writeProblem(w, 503, "Occupancy check failed", err.Error())
		return
	}
	if busy {
		writeProblem(w, 409, "GPU busy", "该卡仍有任务或容器占用，切分前必须结束")
		return
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

func (s *Server) restoreMIGLayouts(ctx context.Context) {
	if !s.migRestoreBusy.CompareAndSwap(false, true) {
		return
	}
	defer s.migRestoreBusy.Store(false)
	layouts, err := s.store.ListMIGLayouts(ctx)
	if err != nil {
		s.setMIGRestoreErrors([]string{"读取 MIG 布局失败: " + err.Error()})
		return
	}
	topology, err := s.liveTopology(ctx)
	if err != nil {
		s.setMIGRestoreErrors([]string{"拓扑不可用，无法重建 MIG: " + err.Error()})
		return
	}
	var restoreErrors []string
	for _, layout := range layouts {
		if !layout.Enabled {
			continue
		}
		if err := s.restoreOneMIGLayout(ctx, topology, layout); err != nil {
			restoreErrors = append(restoreErrors, err.Error())
			s.log.Error("restore MIG layout", "gpu", layout.GPUUUID, "error", err)
		}
	}
	s.setMIGRestoreErrors(restoreErrors)
}

func (s *Server) restoreOneMIGLayout(ctx context.Context, topology domain.Topology, layout domain.MIGLayout) error {
	gpu, children, ok := findPhysicalGPU(topology, layout.GPUUUID)
	if !ok {
		return fmt.Errorf("GPU %s 不在拓扑中，跳过重建 MIG", layout.GPUUUID)
	}
	live := liveInstanceProfiles(topology, layout.GPUUUID)
	if sameProfiles(live, layout.Profiles) {
		return nil
	}
	busy, err := s.gpuUUIDsBusy(ctx, append([]string{layout.GPUUUID}, children...))
	if err != nil {
		return fmt.Errorf("GPU %s 占用检查失败: %w", shortGPU(layout.GPUUUID), err)
	}
	if busy {
		return fmt.Errorf("GPU %d 有任务占用，跳过重建 MIG 布局", gpu.Index)
	}
	if err := s.agent.ApplyMIG(ctx, layout.GPUUUID, gpu.MemoryBytes, layout.Profiles); err != nil {
		return fmt.Errorf("GPU %d 重建 MIG 失败: %w", gpu.Index, err)
	}
	s.log.Info("restored MIG layout", "gpu", layout.GPUUUID, "profiles", mig.FormatSlices(layout.Profiles))
	return nil
}

func (s *Server) gpuUUIDsBusy(ctx context.Context, uuids []string) (bool, error) {
	blocked := make(map[string]struct{}, len(uuids))
	for _, uuid := range uuids {
		blocked[uuid] = struct{}{}
	}
	allocations, err := s.store.ListActiveAllocations(ctx)
	if err != nil {
		return false, err
	}
	for _, allocation := range allocations {
		if allocation.ResourceType != "gpu" {
			continue
		}
		if _, busy := blocked[allocation.ResourceID]; busy {
			return true, nil
		}
	}
	claims, err := s.agent.Claims(ctx)
	if err != nil {
		return false, err
	}
	for _, claim := range claims {
		if !strings.EqualFold(claim.State, "running") && !strings.EqualFold(claim.State, "created") && !strings.EqualFold(claim.State, "paused") && !strings.EqualFold(claim.State, "restarting") {
			continue
		}
		for _, uuid := range append(claim.GPUUUIDs, claim.TrainingGPUUUIDs...) {
			if _, busy := blocked[uuid]; busy {
				return true, nil
			}
		}
	}
	return false, nil
}

func (s *Server) setMIGRestoreErrors(errors []string) {
	if errors == nil {
		errors = []string{}
	}
	s.migRestoreMu.Lock()
	s.migRestoreErrors = errors
	s.migRestoreMu.Unlock()
}

func findPhysicalGPU(topology domain.Topology, gpuUUID string) (domain.GPU, []string, bool) {
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
	return gpu, children, found
}

func liveInstanceProfiles(topology domain.Topology, parentUUID string) []string {
	profiles := make([]string, 0)
	for _, gpu := range topology.GPUs {
		if gpu.ParentUUID == parentUUID {
			profiles = append(profiles, gpu.Profile)
		}
	}
	sort.Strings(profiles)
	return profiles
}

func sameProfiles(live, saved []string) bool {
	want := append([]string{}, saved...)
	sort.Strings(want)
	return slices.Equal(live, want)
}

func shortGPU(uuid string) string {
	if len(uuid) > 8 {
		return uuid[:8]
	}
	return uuid
}
