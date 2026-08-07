package headlessruntime

import "testing"

func TestStatusOnlineState(t *testing.T) {
	cases := []struct {
		status string
		want   OnlineState
	}{
		{StatusBusy, OnlineTrue},
		{StatusIdle, OnlineTrue},
		{StatusOnline, OnlineTrue},
		{StatusOffline, OnlineFalse},
		{StatusStarting, OnlineFalse},
		{StatusStopping, OnlineFalse},
		{StatusStopped, OnlineFalse},
		{StatusDeleted, OnlineFalse},
		{" busy ", OnlineTrue},
		{"", OnlineUnknown},
		{"unknown", OnlineUnknown},
	}
	for _, tc := range cases {
		if got := StatusOnlineState(tc.status); got != tc.want {
			t.Fatalf("StatusOnlineState(%q) = %v, want %v", tc.status, got, tc.want)
		}
	}
}
