package stagedstreamsync

import "testing"

func TestLimitCycleTargetHeight(t *testing.T) {
	const (
		currentHeight = uint64(92730035)
		networkTarget = uint64(92731059)
	)

	tests := []struct {
		name  string
		limit uint64
		want  uint64
	}{
		{name: "one block", limit: 1, want: 92730036},
		{name: "smaller than distance", limit: 512, want: 92730547},
		{name: "larger than distance", limit: 2048, want: networkTarget},
		{name: "unlimited", limit: 0, want: networkTarget},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := limitCycleTargetHeight(currentHeight, networkTarget, test.limit); got != test.want {
				t.Fatalf("limitCycleTargetHeight(%d, %d, %d) = %d, want %d", currentHeight, networkTarget, test.limit, got, test.want)
			}
		})
	}
}
