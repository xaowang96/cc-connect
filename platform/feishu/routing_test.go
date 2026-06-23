package feishu

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/chenhg5/cc-connect/core"
	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

// setupSharedGroup wires 2 platforms into a fresh shared group for routing tests.
// Returns (wildcard, concrete) so tests can drive either end.
func setupSharedGroup(t *testing.T, wildcardAllow, concreteAllow string) (*Platform, *Platform) {
	t.Helper()
	g := &sharedWSGroup{}
	wc := &Platform{platformName: "feishu", allowChat: wildcardAllow, sharedGroup: g}
	cc := &Platform{platformName: "feishu", allowChat: concreteAllow, sharedGroup: g}
	g.platforms = []*Platform{wc, cc}
	return wc, cc
}

func TestIsAllowChatWildcard(t *testing.T) {
	cases := map[string]bool{
		"":          true,
		"*":         true,
		" * ":       true,
		"oc_x":      false,
		"oc_x,oc_y": false,
	}
	for in, want := range cases {
		if got := isAllowChatWildcard(in); got != want {
			t.Errorf("isAllowChatWildcard(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestChatClaimedByConcretePeer_WildcardYieldsToConcrete(t *testing.T) {
	wc, _ := setupSharedGroup(t, "*", "oc_opencode_group")

	if !chatClaimedByConcretePeer(wc, "oc_opencode_group") {
		t.Fatal("wildcard project must detect concrete peer claiming the chat")
	}
}

func TestChatClaimedByConcretePeer_UnclaimedChatStaysWithWildcard(t *testing.T) {
	wc, _ := setupSharedGroup(t, "*", "oc_opencode_group")

	if chatClaimedByConcretePeer(wc, "oc_brand_new_group") {
		t.Fatal("unclaimed chat must not be treated as claimed; wildcard keeps it")
	}
	if chatClaimedByConcretePeer(wc, "oc_p2p_private") {
		t.Fatal("p2p chat not claimed by any peer must stay with wildcard")
	}
}

func TestChatClaimedByConcretePeer_EmptyAllowChatTreatedAsWildcard(t *testing.T) {
	// Peer with allow_chat="" is also wildcard — should not block anyone.
	g := &sharedWSGroup{}
	wc := &Platform{platformName: "feishu", allowChat: "*", sharedGroup: g}
	openPeer := &Platform{platformName: "feishu", allowChat: "", sharedGroup: g}
	g.platforms = []*Platform{wc, openPeer}

	if chatClaimedByConcretePeer(wc, "oc_anything") {
		t.Fatal("empty allow_chat is wildcard; must not claim chats")
	}
}

func TestChatClaimedByConcretePeer_ConcreteListMatchingOne(t *testing.T) {
	wc, _ := setupSharedGroup(t, "*", "oc_a,oc_b,oc_c")

	for _, id := range []string{"oc_a", "oc_b", "oc_c"} {
		if !chatClaimedByConcretePeer(wc, id) {
			t.Errorf("peer list %q must claim %q", "oc_a,oc_b,oc_c", id)
		}
	}
	if chatClaimedByConcretePeer(wc, "oc_d") {
		t.Fatal("chat outside concrete list must not be claimed")
	}
}

func TestChatClaimedByConcretePeer_SelfIsNotPeer(t *testing.T) {
	// A platform with concrete allow_chat should NOT block itself.
	g := &sharedWSGroup{}
	only := &Platform{platformName: "feishu", allowChat: "oc_x", sharedGroup: g}
	g.platforms = []*Platform{only}

	if chatClaimedByConcretePeer(only, "oc_x") {
		t.Fatal("a platform must never consider itself a peer claim")
	}
}

func TestChatClaimedByConcretePeer_NoSharedGroup(t *testing.T) {
	lone := &Platform{platformName: "feishu", allowChat: "*"}
	if chatClaimedByConcretePeer(lone, "oc_x") {
		t.Fatal("platform without sharedGroup must return false")
	}
	if chatClaimedByConcretePeer(nil, "oc_x") {
		t.Fatal("nil platform must return false")
	}
}

// E2E: onMessage honors peer claims across the shared WS group.
func TestOnMessage_SharedGroupRouting(t *testing.T) {
	newInteractive := func(allowFrom, allowChat string) (*interactivePlatform, *Platform) {
		p, err := newPlatform("feishu", lark.FeishuBaseUrl, map[string]any{
			"app_id": "cli_shared_routing", "app_secret": "secret",
			"enable_feishu_card": true,
			"group_reply_all":    true, // skip @bot mention check in group
			"allow_from":         allowFrom,
			"allow_chat":         allowChat,
		})
		if err != nil {
			t.Fatalf("newPlatform: %v", err)
		}
		ip := p.(*interactivePlatform)
		return ip, ip.Platform
	}

	userID := "ou_sender"
	senderType := "user"
	msgType := "text"
	content := `{"text":"hi"}`

	// Wire 2 sibling platforms into the same sharedWSGroup (simulating
	// same-app_id multi-project deployment).
	wildIP, wildPL := newInteractive(userID, "*")
	concreteIP, concretePL := newInteractive(userID, "oc_opencode_group")
	group := &sharedWSGroup{platforms: []*Platform{wildPL, concretePL}}
	wildPL.sharedGroup = group
	concretePL.sharedGroup = group

	deliver := func(ip *interactivePlatform, chatID, chatType string) bool {
		t.Helper()
		msgCh := make(chan *core.Message, 1)
		ip.handler = func(_ core.Platform, m *core.Message) { msgCh <- m }
		messageID := "om_" + chatID + "_" + chatType + "_" + ip.allowChat
		createTime := strconv.FormatInt(time.Now().UnixMilli(), 10)
		err := ip.onMessage(context.Background(), &larkim.P2MessageReceiveV1{
			Event: &larkim.P2MessageReceiveV1Data{
				Sender: &larkim.EventSender{SenderId: &larkim.UserId{OpenId: &userID}, SenderType: &senderType},
				Message: &larkim.EventMessage{
					MessageId: &messageID, ChatId: &chatID, ChatType: &chatType,
					MessageType: &msgType, Content: &content, CreateTime: &createTime,
				},
			},
		})
		if err != nil {
			t.Fatalf("onMessage: %v", err)
		}
		select {
		case <-msgCh:
			return true
		case <-time.After(500 * time.Millisecond):
			return false
		}
	}

	// Claimed group: concrete owns it; wildcard yields.
	if !deliver(concreteIP, "oc_opencode_group", "group") {
		t.Fatal("concrete platform must process its claimed group")
	}
	if deliver(wildIP, "oc_opencode_group", "group") {
		t.Fatal("wildcard platform must yield when peer claims the chat")
	}

	// Unclaimed new group: concrete rejects (not listed), wildcard accepts.
	if deliver(concreteIP, "oc_brand_new", "group") {
		t.Fatal("concrete platform must reject unlisted group")
	}
	if !deliver(wildIP, "oc_brand_new", "group") {
		t.Fatal("wildcard platform must accept unclaimed new group (auto-adoption)")
	}

	// P2P now subject to same routing.
	if deliver(concreteIP, "oc_some_p2p", "p2p") {
		t.Fatal("concrete platform must reject p2p not in its allow_chat (post-fix)")
	}
	if !deliver(wildIP, "oc_some_p2p", "p2p") {
		t.Fatal("wildcard platform must accept unclaimed p2p")
	}
}
