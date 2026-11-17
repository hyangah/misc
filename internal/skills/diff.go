// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package skills

import (
	"bytes"
	"fmt"
	"slices"
	"strings"
)

// unifiedDiff generates a unified diff between orig (file on disk) and want (embedded file).
// origName is the label for the original file, wantName is the label for the new file.
func unifiedDiff(origName, wantName string, orig, want []byte) string {
	if bytes.Equal(orig, want) {
		return ""
	}

	a := splitLines(string(orig))
	b := splitLines(string(want))

	n, m := len(a), len(b)
	dp := make([][]int, n+1)
	for i := range dp {
		dp[i] = make([]int, m+1)
	}
	for i := 0; i < n; i++ {
		for j := 0; j < m; j++ {
			if a[i] == b[j] {
				dp[i+1][j+1] = dp[i][j] + 1
			} else if dp[i+1][j] >= dp[i][j+1] {
				dp[i+1][j+1] = dp[i+1][j]
			} else {
				dp[i+1][j+1] = dp[i][j+1]
			}
		}
	}

	type editType int
	const (
		editEqual editType = iota
		editDelete
		editInsert
	)
	type edit struct {
		typ  editType
		line string
		aIdx int // 1-based line in a
		bIdx int // 1-based line in b
	}

	var edits []edit
	i, j := n, m
	for i > 0 || j > 0 {
		if i > 0 && j > 0 && a[i-1] == b[j-1] {
			edits = append(edits, edit{typ: editEqual, line: a[i-1], aIdx: i, bIdx: j})
			i--
			j--
		} else if j > 0 && (i == 0 || dp[i][j-1] >= dp[i-1][j]) {
			edits = append(edits, edit{typ: editInsert, line: b[j-1], bIdx: j})
			j--
		} else if i > 0 && (j == 0 || dp[i][j-1] < dp[i-1][j]) {
			edits = append(edits, edit{typ: editDelete, line: a[i-1], aIdx: i})
			i--
		}
	}
	slices.Reverse(edits)

	type hunkRange struct {
		start int
		end   int
	}

	const contextLines = 3
	var hunkRanges []hunkRange
	for idx, ed := range edits {
		if ed.typ != editEqual {
			start := max(0, idx-contextLines)
			end := min(len(edits)-1, idx+contextLines)
			if len(hunkRanges) > 0 && start <= hunkRanges[len(hunkRanges)-1].end+1 {
				hunkRanges[len(hunkRanges)-1].end = end
			} else {
				hunkRanges = append(hunkRanges, hunkRange{start: start, end: end})
			}
		}
	}

	if len(hunkRanges) == 0 {
		return ""
	}

	var buf strings.Builder
	buf.WriteString(fmt.Sprintf("--- %s\n", origName))
	buf.WriteString(fmt.Sprintf("+++ %s\n", wantName))

	for _, hr := range hunkRanges {
		aStart := 0
		aCount := 0
		bStart := 0
		bCount := 0

		for k := hr.start; k <= hr.end; k++ {
			ed := edits[k]
			switch ed.typ {
			case editEqual:
				if aStart == 0 {
					aStart = ed.aIdx
				}
				if bStart == 0 {
					bStart = ed.bIdx
				}
				aCount++
				bCount++
			case editDelete:
				if aStart == 0 {
					aStart = ed.aIdx
				}
				aCount++
			case editInsert:
				if bStart == 0 {
					bStart = ed.bIdx
				}
				bCount++
			}
		}

		if aCount == 0 {
			aStart = 0
		}
		if bCount == 0 {
			bStart = 0
		}

		buf.WriteString(fmt.Sprintf("@@ -%d,%d +%d,%d @@\n", aStart, aCount, bStart, bCount))
		for k := hr.start; k <= hr.end; k++ {
			ed := edits[k]
			line := ed.line
			hasNewline := strings.HasSuffix(line, "\n")
			if !hasNewline {
				line += "\n"
			}
			switch ed.typ {
			case editEqual:
				buf.WriteString(" " + line)
			case editDelete:
				buf.WriteString("-" + line)
			case editInsert:
				buf.WriteString("+" + line)
			}
			if !hasNewline {
				buf.WriteString("\\ No newline at end of file\n")
			}
		}
	}

	return buf.String()
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	raw := strings.SplitAfter(s, "\n")
	if len(raw) > 0 && raw[len(raw)-1] == "" {
		raw = raw[:len(raw)-1]
	}
	return raw
}
