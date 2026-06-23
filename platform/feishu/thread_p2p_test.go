package feishu

import (
	"errors"
	"testing"
)

func TestShouldReplyInThread_P2PHonorsIsolationFlag(t *testing.T) {
	// p2p session key (user-keyed, no root/thread tail).
	rc := replyContext{
		messageID:  "om_trigger",
		chatID:     "oc_p2p",
		sessionKey: "feishu:oc_p2p:ou_user",
	}

	p := &Platform{threadIsolation: false}
	if p.shouldReplyInThread(rc) {
		t.Fatal("p2p without threadIsolation should not reply_in_thread")
	}

	p = &Platform{threadIsolation: true}
	if !p.shouldReplyInThread(rc) {
		t.Fatal("p2p with threadIsolation=true should reply_in_thread (regression: feishu p2p accepts reply_in_thread)")
	}
}

func TestShouldReplyInThread_GroupRootKeyedStillWorks(t *testing.T) {
	rc := replyContext{
		messageID:  "om_trigger",
		chatID:     "oc_group",
		sessionKey: "feishu:oc_group:root:om_trigger",
	}
	p := &Platform{threadIsolation: true}
	if !p.shouldReplyInThread(rc) {
		t.Fatal("group with root-keyed session must reply_in_thread")
	}
}

func TestShouldReplyInThread_NoMessageIDOrEmptyChat(t *testing.T) {
	p := &Platform{threadIsolation: true}
	if p.shouldReplyInThread(replyContext{chatID: "oc_x"}) {
		t.Fatal("missing messageID must disable thread reply")
	}
}

func TestShouldReplyInThread_DenylistedChatSkipsThread(t *testing.T) {
	p := &Platform{threadIsolation: true}
	rc := replyContext{
		messageID:  "om_trigger",
		chatID:     "oc_bad",
		sessionKey: "feishu:oc_bad:ou_user",
	}
	if !p.shouldReplyInThread(rc) {
		t.Fatal("precondition: thread should be enabled before denylist")
	}
	p.markThreadUnsupported(rc.chatID)
	if p.shouldReplyInThread(rc) {
		t.Fatal("after markThreadUnsupported, thread must be skipped for this chat")
	}
}

func TestMarkThreadUnsupported_IgnoresEmpty(t *testing.T) {
	p := &Platform{threadIsolation: true}
	p.markThreadUnsupported("")
	// Unrelated chat must stay enabled.
	rc := replyContext{messageID: "om_x", chatID: "oc_other", sessionKey: "feishu:oc_other:ou_u"}
	if !p.shouldReplyInThread(rc) {
		t.Fatal("empty chatID must not poison denylist")
	}
}

func TestIsThreadUnsupportedErr_MatchesCode230071(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"unrelated", errors.New("feishu: reply failed code=99991668 msg=rate limited"), false},
		{"exact", errors.New("feishu: reply failed code=230071 msg=..."), true},
		{"wrapped preview", errors.New("feishu: send preview (reply) code=230071 msg=..."), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isThreadUnsupportedErr(tc.err); got != tc.want {
				t.Fatalf("isThreadUnsupportedErr(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}
