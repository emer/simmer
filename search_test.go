// Copyright (c) 2026, The Emergent Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"slices"
	"testing"
)

func TestParseSearchN(t *testing.T) {
	out := "conflict: 6 slots\ntest items: [200 200]\n3\nSearch 1: A=1\nSearch 2: A=2\nSearch 3: B=0.5\nconflict_ff func1 succeeded\n"
	ns, lbls, err := parseSearchN(out)
	if err != nil || ns != 3 || !slices.Equal(lbls, []string{"Search 1: A=1", "Search 2: A=2", "Search 3: B=0.5"}) {
		t.Errorf("parseSearchN: %d %q %v", ns, lbls, err)
	}
	ns, lbls, err = parseSearchN("12\n") // older sims: just the number
	if err != nil || ns != 12 || lbls != nil {
		t.Errorf("parseSearchN number only: %d %q %v", ns, lbls, err)
	}
	if _, _, err = parseSearchN("no number\n"); err == nil {
		t.Errorf("no error without a number")
	}
	if _, _, err = parseSearchN("0\n"); err == nil {
		t.Errorf("no error for 0 searches")
	}
}
