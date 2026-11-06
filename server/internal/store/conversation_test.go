package store

import "testing"

// 单聊去重键必须与发起顺序无关，否则并发两端仍会各自插入。
func TestDirectKeyOrderIndependent(t *testing.T) {
	if got := directKey(7, 3); got != "3:7" {
		t.Fatalf("got %q, want 3:7", got)
	}
	if got := directKey(3, 7); got != "3:7" {
		t.Fatalf("got %q, want 3:7", got)
	}
	if got := directKey(9, 9); got != "9:9" {
		t.Fatalf("got %q, want 9:9", got)
	}
}
