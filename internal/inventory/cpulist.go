package inventory

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

func ParseCPUList(value string) ([]int, error) {
	seen := make(map[int]struct{})
	for _, part := range strings.Split(strings.TrimSpace(value), ",") {
		if part == "" {
			continue
		}
		startText, endText, ranged := strings.Cut(part, "-")
		start, err := strconv.Atoi(startText)
		if err != nil || start < 0 {
			return nil, fmt.Errorf("invalid CPU list item %q", part)
		}
		end := start
		if ranged {
			end, err = strconv.Atoi(endText)
			if err != nil || end < start {
				return nil, fmt.Errorf("invalid CPU range %q", part)
			}
		}
		for id := start; id <= end; id++ {
			seen[id] = struct{}{}
		}
	}
	result := make([]int, 0, len(seen))
	for id := range seen {
		result = append(result, id)
	}
	sort.Ints(result)
	return result, nil
}

func FormatCPUList(ids []int) string {
	if len(ids) == 0 {
		return ""
	}
	copyIDs := append([]int(nil), ids...)
	sort.Ints(copyIDs)
	parts := make([]string, 0)
	start, previous := copyIDs[0], copyIDs[0]
	flush := func() {
		if start == previous {
			parts = append(parts, strconv.Itoa(start))
		} else {
			parts = append(parts, fmt.Sprintf("%d-%d", start, previous))
		}
	}
	for _, id := range copyIDs[1:] {
		if id == previous || id == previous+1 {
			previous = id
			continue
		}
		flush()
		start, previous = id, id
	}
	flush()
	return strings.Join(parts, ",")
}
