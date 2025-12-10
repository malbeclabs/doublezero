package qa

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsLossOutsideNetwork(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{
			name: "loss_only_first_hop",
			input: `
Start: 2025-12-10T12:42:45+0000
  1.|-- h1                     10.0%   10  1.0  1.0  1.0  1.0  0.1
  2.|-- h2                      0.0%   10  1.0  1.0  1.0  1.0  0.1
  3.|-- h3                      0.0%   10  1.0  1.0  1.0  1.0  0.1
`,
			want: true,
		},
		{
			name: "loss_only_last_hop",
			input: `
Start: 2025-12-10T12:42:45+0000
  1.|-- h1                      0.0%   10  1.0  1.0  1.0  1.0  0.1
  2.|-- h2                      0.0%   10  1.0  1.0  1.0  1.0  0.1
  3.|-- h3                      5.0%   10  1.0  1.0  1.0  1.0  0.1
`,
			want: true,
		},
		{
			name: "loss_first_and_last",
			input: `
Start: 2025-12-10T12:42:45+0000
  1.|-- h1                     10.0%   10  1.0  1.0  1.0  1.0  0.1
  2.|-- h2                      0.0%   10  1.0  1.0  1.0  1.0  0.1
  3.|-- h3                      5.0%   10  1.0  1.0  1.0  1.0  0.1
`,
			want: false,
		},
		{
			name: "loss_in_middle_only",
			input: `
Start: 2025-12-10T12:42:45+0000
  1.|-- h1                      0.0%   10  1.0  1.0  1.0  1.0  0.1
  2.|-- h2                     10.0%   10  1.0  1.0  1.0  1.0  0.1
  3.|-- h3                      0.0%   10  1.0  1.0  1.0  1.0  0.1
`,
			want: false,
		},
		{
			name: "loss_first_and_middle",
			input: `
Start: 2025-12-10T12:42:45+0000
  1.|-- h1                     10.0%   10  1.0  1.0  1.0  1.0  0.1
  2.|-- h2                     10.0%   10  1.0  1.0  1.0  1.0  0.1
  3.|-- h3                      0.0%   10  1.0  1.0  1.0  1.0  0.1
`,
			want: false,
		},
		{
			name: "no_loss_anywhere",
			input: `
Start: 2025-12-10T12:42:45+0000
  1.|-- h1                      0.0%   10  1.0  1.0  1.0  1.0  0.1
  2.|-- h2                      0.0%   10  1.0  1.0  1.0  1.0  0.1
  3.|-- h3                      0.0%   10  1.0  1.0  1.0  1.0  0.1
`,
			want: false,
		},
		{
			name:  "empty_input",
			input: ``,
			want:  false,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := isLossOutsideNetwork(tc.input)
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestParseMTR(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		wantNums []int
		wantLoss []float64
	}{
		{
			name: "basic_7_hops",
			input: `
Start: 2025-12-10T12:42:45+0000
        HOST: nyc-mn-qa01                 Loss%   Snt   Last   Avg  Best  Wrst StDev
          1.|-- ???                       100.0%    10    0.0   0.0   0.0   0.0   0.0
          2.|-- 172.16.0.70                0.0%    10  329.2 329.4 329.1 331.4   0.7
          3.|-- 172.16.0.42                0.0%    10  332.1 331.5 331.0 332.2   0.5
          4.|-- 172.16.0.9                 0.0%    10  329.5 329.3 329.1 329.7   0.2
          5.|-- 172.16.0.3                 0.0%    10  330.6 330.8 330.6 331.6   0.3
          6.|-- 172.16.0.160               0.0%    10  330.7 330.8 330.6 332.1   0.4
          7.|-- 159.223.46.72             20.0%    10  738.4 738.2 718.8 749.8  10.5
`,
			wantNums: []int{1, 2, 3, 4, 5, 6, 7},
			wantLoss: []float64{100.0, 0.0, 0.0, 0.0, 0.0, 0.0, 20.0},
		},
		{
			name: "skips_non_hop_lines",
			input: `
garbage line
        HOST: something Loss% Snt Last
          1.|-- host1                       10.0%    10    1.0   1.0   1.0   1.0   0.0
this shouldn't match
          2.|-- host2                        0.0%    10    2.0   2.0   2.0   2.0   0.0
`,
			wantNums: []int{1, 2},
			wantLoss: []float64{10.0, 0.0},
		},
		{
			name:     "empty_input",
			input:    ``,
			wantNums: nil,
			wantLoss: nil,
		},
		{
			name: "various_loss_formats",
			input: `
          1.|-- host1                        0%      10   0.0   0.0   0.0   0.0   0.0
          2.|-- host2                       1.0%     10   0.0   0.0   0.0   0.0   0.0
          3.|-- host3                      50.5%     10   0.0   0.0   0.0   0.0   0.0
`,
			wantNums: []int{1, 2, 3},
			wantLoss: []float64{0.0, 1.0, 50.5},
		},
		{
			name: "lines_without_percent_ignored",
			input: `
          1.|-- host1                        0      10   0.0   0.0   0.0   0.0   0.0
          2.|-- host2                       10      10   0.0   0.0   0.0   0.0   0.0
`,
			wantNums: nil,
			wantLoss: nil,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			hops, err := parseMTR(tc.input)
			require.NoError(t, err)

			if tc.wantNums == nil {
				require.Len(t, hops, 0)
				return
			}

			require.Len(t, hops, len(tc.wantNums))
			for i := range tc.wantNums {
				require.Equal(t, tc.wantNums[i], hops[i].Num, "hop index %d Num mismatch", i)
				require.InDelta(t, tc.wantLoss[i], hops[i].Loss, 0.0001, "hop %d loss mismatch", hops[i].Num)
			}
		})
	}
}
