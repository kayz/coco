package agent

import (
	"testing"

	"github.com/kayz/coco/internal/router"
)

func TestEnforceMessageSecurityPolicyAllowFrom(t *testing.T) {
	a := &Agent{}
	a.applySecurityConfig(nil, false, nil, nil, []string{"telegram:1001"}, false)

	allowedMsg := router.Message{
		Platform: "telegram",
		UserID:   "1001",
	}
	if denial, drop := a.enforceMessageSecurityPolicy(allowedMsg); drop || denial != "" {
		t.Fatalf("expected allow_from to pass, got denial=%q drop=%v", denial, drop)
	}

	blockedMsg := router.Message{
		Platform: "telegram",
		UserID:   "1002",
	}
	denial, drop := a.enforceMessageSecurityPolicy(blockedMsg)
	if !drop || denial == "" {
		t.Fatalf("expected blocked sender to be rejected, got denial=%q drop=%v", denial, drop)
	}
}

func TestEnforceMessageSecurityPolicyRequireMentionInGroup(t *testing.T) {
	a := &Agent{}
	a.applySecurityConfig(nil, false, nil, nil, nil, true)

	groupNoMention := router.Message{
		Platform: "telegram",
		Metadata: map[string]string{
			"chat_type": "group",
		},
		Text: "hello",
	}
	if denial, drop := a.enforceMessageSecurityPolicy(groupNoMention); !drop || denial != "" {
		t.Fatalf("expected group message without mention to be dropped, got denial=%q drop=%v", denial, drop)
	}

	groupMention := router.Message{
		Platform: "telegram",
		Metadata: map[string]string{
			"chat_type": "group",
			"mentioned": "true",
		},
		Text: "hello",
	}
	if denial, drop := a.enforceMessageSecurityPolicy(groupMention); drop || denial != "" {
		t.Fatalf("expected mentioned group message to pass, got denial=%q drop=%v", denial, drop)
	}

	privateMsg := router.Message{
		Platform: "telegram",
		Metadata: map[string]string{
			"chat_type": "private",
		},
		Text: "hello",
	}
	if denial, drop := a.enforceMessageSecurityPolicy(privateMsg); drop || denial != "" {
		t.Fatalf("expected private message to pass, got denial=%q drop=%v", denial, drop)
	}
}
