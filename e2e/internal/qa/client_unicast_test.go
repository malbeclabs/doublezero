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
			name: "loss_only_first_hop_with_host_header",
			input: `
Start: 2025-12-12T06:56:27+0000
HOST: fra-mn-qa01                 Loss%   Snt   Last   Avg  Best  Wrst StDev
	1.|-- ???                       100.0    10    0.0   0.0   0.0   0.0   0.0
	2.|-- 172.16.0.79                0.0%    10  385.1 388.3 343.3 398.8  16.4
	3.|-- 172.16.0.21                0.0%    10  386.3 385.7 341.8 403.0  16.5
	4.|-- 172.16.0.30                0.0%    10  392.3 386.2 354.4 413.0  14.1
	5.|-- 172.16.0.5                 0.0%    10  402.5 393.5 374.7 410.4   9.9
	6.|-- 172.16.0.1                 0.0%    10  410.6 406.7 397.4 410.8   4.6
	7.|-- 172.16.0.15                0.0%    10  414.6 414.8 414.5 415.7   0.4
	8.|-- 172.16.0.56                0.0%    10  416.5 413.0 410.5 416.5   1.4
	9.|-- 172.16.0.76                0.0%    10  414.8 412.7 402.4 415.1   4.3
	10.|-- 159.223.46.72              0.0%    10  647.7 666.1 630.6 694.0  22.0
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

		{
			name: "single_hop_with_loss_is_not_outside_network",
			input: `
		  Start: 2025-12-10T12:42:45+0000
			1.|-- h1                     10.0%   10  1.0  1.0  1.0  1.0  0.1
		  `,
			want: false,
		},
		{
			name: "single_hop_no_loss",
			input: `
		  Start: 2025-12-10T12:42:45+0000
			1.|-- h1                      0.0%   10  1.0  1.0  1.0  1.0  0.1
		  `,
			want: false,
		},
		{
			name: "two_hops_loss_only_first",
			input: `
		  Start: 2025-12-10T12:42:45+0000
			1.|-- h1                     10.0%   10  1.0  1.0  1.0  1.0  0.1
			2.|-- h2                      0.0%   10  1.0  1.0  1.0  1.0  0.1
		  `,
			want: true,
		},
		{
			name: "two_hops_loss_only_last",
			input: `
		  Start: 2025-12-10T12:42:45+0000
			1.|-- h1                      0.0%   10  1.0  1.0  1.0  1.0  0.1
			2.|-- h2                      5.0%   10  1.0  1.0  1.0  1.0  0.1
		  `,
			want: true,
		},
		{
			name: "two_hops_loss_both",
			input: `
		  Start: 2025-12-10T12:42:45+0000
			1.|-- h1                     10.0%   10  1.0  1.0  1.0  1.0  0.1
			2.|-- h2                      5.0%   10  1.0  1.0  1.0  1.0  0.1
		  `,
			want: false,
		},
		{
			name: "non_empty_but_no_hops_parsed",
			input: `
		  Start: 2025-12-10T12:42:45+0000
		  HOST: example Loss% Snt Last
		  (no hop lines here)
		  `,
			want: false,
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
			name: "lines_without_percent_are_accepted",
			input: `
				  1.|-- host1                        0      10   0.0   0.0   0.0   0.0   0.0
				  2.|-- host2                       10      10   0.0   0.0   0.0   0.0   0.0
		`,
			wantNums: []int{1, 2},
			wantLoss: []float64{0.0, 10.0},
		},
		{
			name:     "tabs_and_spacing",
			input:    "Start: x\n\t1.|--\thost1\t\t10.0%\t10\t0.0\t0.0\t0.0\t0.0\t0.0\n\t2.|--\thost2\t\t0.0\t10\t0.0\t0.0\t0.0\t0.0\t0.0\n",
			wantNums: []int{1, 2},
			wantLoss: []float64{10.0, 0.0},
		},
		{
			name: "hostnames_with_punctuation",
			input: `
			1.|-- edge-1.example.com       0.0%    10  1.0  1.0  1.0  1.0  0.1
			2.|-- 2606:4700:4700::1111     1.0%    10  1.0  1.0  1.0  1.0  0.1
		  `,
			wantNums: []int{1, 2},
			wantLoss: []float64{0.0, 1.0},
		},
		{
			name: "loss_without_leading_zero_is_ignored",
			input: `
			1.|-- h1                       .5%    10  0.0  0.0  0.0  0.0  0.0
			2.|-- h2                       0.5%   10  0.0  0.0  0.0  0.0  0.0
		  `,
			wantNums: []int{2},
			wantLoss: []float64{0.5},
		},
		{
			name: "rejects_when_snt_not_numeric",
			input: `
			1.|-- h1                       10.0%   xx  0.0  0.0  0.0  0.0  0.0
			2.|-- h2                        0.0%   10  0.0  0.0  0.0  0.0  0.0
		  `,
			wantNums: []int{2},
			wantLoss: []float64{0.0},
		},
		{
			name: "indented_hops",
			input: `
					  1.|-- h1              0%      10  0.0  0.0  0.0  0.0  0.0
					  2.|-- h2              2%      10  0.0  0.0  0.0  0.0  0.0
		  `,
			wantNums: []int{1, 2},
			wantLoss: []float64{0.0, 2.0},
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
