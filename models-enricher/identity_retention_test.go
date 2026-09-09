package main

import (
	"net/http"
	"testing"
)

func TestCatalogRetainsOnlyManagementMatches(t *testing.T) {
	cfg := testCfg()
	cfg.Channels = nil
	fake := &fakeCPA{
		native: []byte(`{"models":[{"slug":"vendor/model"},{"slug":"hub/vendor/model"},
			{"slug":"hub/missing"},{"slug":"another/vendor/model"},{"slug":"unknown"},{"slug":"empty/model"}]}`),
		channelsBody: []byte(`{"openai-compatibility":[
			{"prefix":"hub","base-url":"https://unused.invalid","api-key-entries":[{}],"models":[{"name":"vendor/model"},{"name":"not-public"}]},
			{"prefix":"empty","base-url":"https://unused.invalid","api-key-entries":[{}],"models":[]}
		]}`),
	}
	h, _ := identityHandler(t, fake, nil, cfg, nil)
	got := identityCatalog(t, h)
	if len(got) != 1 || got["hub/vendor/model"] == nil {
		t.Fatalf("successful lookup must retain only the exact channel match: %#v", got)
	}
}

func TestCatalogOAuthRetentionMatchesDefinitionsWithoutBareRegistration(t *testing.T) {
	cfg := testCfg()
	cfg.Channels = nil
	fake := &fakeCPA{native: []byte(`{"models":[
		{"slug":"vendor/model"},{"slug":"named"},{"slug":"session/vendor/model","id":"original-id","null":null,"n":9007199254740993},{"slug":"session/named"},
		{"slug":"session/alien"},{"slug":"other/vendor/model"},{"slug":"session/unknown"},{"slug":"beta/alien"}
	]}`)}
	extra := func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v0/management/auth-files":
			w.Write([]byte(`{"files":[{"name":"a.json","provider":"codex"},{"name":"b.json","provider":"codex"}]}`))
		case "/v0/management/auth-files/models":
			// force-model-prefix模式：CPA只注册带前缀ID，无裸名副本。
			switch r.URL.Query().Get("name") {
			case "a.json":
				w.Write([]byte(`{"models":[{"id":"session/vendor/model"},{"id":"session/named"},{"id":"session/alien"},{"id":"session/unknown"},{"id":"session/not-public"}]}`))
			case "b.json":
				w.Write([]byte(`{"models":[{"id":"beta/alien"}]}`))
			default:
				t.Error("registration lost account association")
			}
		case "/v0/management/model-definitions/codex":
			w.Write([]byte(`{"models":[{"id":"vendor/model"},{"id":"alien"},{"id":"not-public"}]}`))
		case "/v0/management/oauth-model-alias":
			w.Write([]byte(`{"oauth-model-alias":{"codex":[{"name":"vendor/model","alias":"named"}]}}`))
		default:
			t.Errorf("unexpected identity endpoint: %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}
	h, _ := identityHandler(t, fake, extra, cfg, nil)
	got := identityCatalog(t, h)
	// 命中definitions/别名的带前缀注册ID全部保留；session/unknown无定义被剔；
	// other/vendor/model未注册被剔；裸原名/裸别名本身不作为公开成员。
	if len(got) != 4 || got["session/vendor/model"] == nil || got["session/named"] == nil || got["session/alien"] == nil || got["beta/alien"] == nil {
		t.Fatalf("prefixed registrations matching definitions must survive without bare counterparts: %#v", got)
	}
	assertMetadataJSON(t, got["session/vendor/model"], `{"slug":"session/vendor/model","id":"original-id","null":null,"n":9007199254740993}`)
}

func TestCatalogRetentionCannotBeUndoneByLegacyInventory(t *testing.T) {
	cfg := &Config{Channels: map[string]ChannelConfig{"p": {}}}
	ids := &catalogIdentities{qualified: map[string]bool{"p/allowed": true}}
	base, _ := ids.filter(&Manifest{Models: []map[string]any{{"slug": "p/allowed"}, {"slug": "p/rejected"}}}, cfg)
	out := mergeManifest(base, []channelModels{{
		Channel: Channel{Type: "gemini-api-key", Prefix: "p"},
		Models:  []ParsedModel{{ID: "allowed"}, {ID: "rejected"}},
	}}, cfg, emptySourceTables(), nil)
	if len(out.Models) != 1 || out.Models[0]["slug"] != "p/allowed" {
		t.Fatalf("inventory reintroduced a model rejected by positive admission: %#v", out.Models)
	}
}
