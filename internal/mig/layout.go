package mig

import (
	"fmt"
	"strings"

	"gpu-scheduler/internal/domain"
)

const TotalG = 7

type Profile struct {
	Name     string
	G        int
	MemoryGB int
	MaxCount int
	GIID     int
}

type Catalog struct {
	Family   string
	MemoryGB int
	Profiles []Profile
}

var (
	family80 = Catalog{
		Family: "80g", MemoryGB: 80,
		Profiles: []Profile{
			{Name: "7g.80gb", G: 7, MemoryGB: 80, MaxCount: 1, GIID: 0},
			{Name: "4g.40gb", G: 4, MemoryGB: 40, MaxCount: 1, GIID: 5},
			{Name: "3g.40gb", G: 3, MemoryGB: 40, MaxCount: 2, GIID: 9},
			{Name: "2g.20gb", G: 2, MemoryGB: 20, MaxCount: 3, GIID: 14},
			{Name: "1g.10gb", G: 1, MemoryGB: 10, MaxCount: 7, GIID: 19},
		},
	}
	family40 = Catalog{
		Family: "40g", MemoryGB: 40,
		Profiles: []Profile{
			{Name: "7g.40gb", G: 7, MemoryGB: 40, MaxCount: 1, GIID: 0},
			{Name: "4g.20gb", G: 4, MemoryGB: 20, MaxCount: 1, GIID: 5},
			{Name: "3g.20gb", G: 3, MemoryGB: 20, MaxCount: 2, GIID: 9},
			{Name: "2g.10gb", G: 2, MemoryGB: 10, MaxCount: 3, GIID: 14},
			{Name: "1g.5gb", G: 1, MemoryGB: 5, MaxCount: 7, GIID: 19},
		},
	}
)

func ProfileG(name string) (int, bool) {
	if profile, ok := family80.ByName(name); ok {
		return profile.G, true
	}
	if profile, ok := family40.ByName(name); ok {
		return profile.G, true
	}
	return 0, false
}

func KnownProfile(name string) bool {
	_, ok := ProfileG(name)
	return ok
}

func Units(gpu domain.GPU) int {
	if gpu.ParentUUID != "" {
		if g, ok := ProfileG(gpu.Profile); ok {
			return g
		}
	}
	return TotalG
}

func CatalogForMemory(memoryBytes uint64) Catalog {
	if memoryBytes >= 60*1024*1024*1024 {
		return family80
	}
	return family40
}

func (c Catalog) ByName(name string) (Profile, bool) {
	for _, profile := range c.Profiles {
		if profile.Name == name {
			return profile, true
		}
	}
	return Profile{}, false
}

type Plan struct {
	UsedG        int
	RemainG      int
	UsedMemoryGB int
	RemainMemGB  int
	Selectable   []string
}

func Evaluate(catalog Catalog, slices []string) (Plan, error) {
	usedG, usedMem := 0, 0
	counts := map[string]int{}
	for _, name := range slices {
		profile, ok := catalog.ByName(name)
		if !ok {
			return Plan{}, fmt.Errorf("不支持的档位 %s", name)
		}
		counts[name]++
		if counts[name] > profile.MaxCount {
			return Plan{}, fmt.Errorf("%s 最多 %d 片", profile.Name, profile.MaxCount)
		}
		usedG += profile.G
		usedMem += profile.MemoryGB
		if usedG > TotalG {
			return Plan{}, fmt.Errorf("计算切片超过 7g")
		}
		if usedMem > catalog.MemoryGB {
			return Plan{}, fmt.Errorf("显存超过 %d GB", catalog.MemoryGB)
		}
	}
	plan := Plan{
		UsedG: usedG, RemainG: TotalG - usedG,
		UsedMemoryGB: usedMem, RemainMemGB: catalog.MemoryGB - usedMem,
	}
	for _, profile := range catalog.Profiles {
		if canAdd(catalog, counts, usedG, usedMem, profile) {
			plan.Selectable = append(plan.Selectable, profile.Name)
		}
	}
	return plan, nil
}

func canAdd(catalog Catalog, counts map[string]int, usedG, usedMem int, profile Profile) bool {
	if counts[profile.Name]+1 > profile.MaxCount {
		return false
	}
	if usedG+profile.G > TotalG {
		return false
	}
	if usedMem+profile.MemoryGB > catalog.MemoryGB {
		return false
	}
	return true
}

func GIIDs(catalog Catalog, slices []string) ([]int, error) {
	ids := make([]int, 0, len(slices))
	for _, name := range slices {
		profile, ok := catalog.ByName(name)
		if !ok {
			return nil, fmt.Errorf("不支持的档位 %s", name)
		}
		ids = append(ids, profile.GIID)
	}
	return ids, nil
}

func FormatSlices(slices []string) string {
	if len(slices) == 0 {
		return "整卡"
	}
	return strings.Join(slices, " + ")
}
