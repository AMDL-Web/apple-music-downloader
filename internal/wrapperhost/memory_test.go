package wrapperhost

import (
	"fmt"
	"testing"
)

func TestGuestMemoryScalesAndCaps(t *testing.T) {
	for _, tc := range []struct{ accounts, want int }{{-1, 384}, {0, 384}, {1, 480}, {3, 672}, {10, 1344}, {11, 1344}} {
		t.Run(fmt.Sprint(tc.accounts), func(t *testing.T) {
			if got := GuestMemoryMB(tc.accounts); got != tc.want {
				t.Fatalf("memory %d MB, want %d MB", got, tc.want)
			}
		})
	}
}

func TestRegistryCountIncludesDegradedAccounts(t *testing.T) {
	n, err := registryCount([]byte(`[{"id":"one","region":"us"},{"id":"two","region":""},{"id":"three","degraded":true}]`))
	if err != nil || n != 3 {
		t.Fatalf("registry count %d, %v", n, err)
	}
	if n, err := registryCount([]byte(`[]`)); err != nil || n != 0 {
		t.Fatalf("empty registry %d, %v", n, err)
	}
	if _, err := registryCount([]byte(`not JSON`)); err == nil {
		t.Fatal("corrupt registry accepted")
	}
}
