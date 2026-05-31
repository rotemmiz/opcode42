package tool

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rotemmiz/forge/internal/engine/question"
)

func TestWebFetch_StripsHTML(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html><head><style>x{}</style></head><body><p>Hello</p><script>1</script></body></html>"))
	}))
	defer srv.Close()
	res, err := WebFetch{HTTPClient: srv.Client()}.Run(context.Background(), map[string]any{"url": srv.URL}, tctx(""))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(res.Output, "<") || !strings.Contains(res.Output, "Hello") {
		t.Fatalf("html not reduced: %q", res.Output)
	}
}

func TestWebFetch_RejectsNonHTTP(t *testing.T) {
	if _, err := (WebFetch{}).Run(context.Background(), map[string]any{"url": "ftp://x"}, tctx("")); err == nil {
		t.Fatal("expected non-http rejection")
	}
}

func TestTodoWrite_StoresAndRenders(t *testing.T) {
	store := NewTodoStore()
	res, err := TodoWrite{Store: store}.Run(context.Background(), map[string]any{
		"todos": []any{
			map[string]any{"content": "build", "status": "in_progress"},
			map[string]any{"content": "test", "status": "pending"},
		},
	}, tctx(""))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Output, "[in_progress] build") {
		t.Fatalf("render wrong: %q", res.Output)
	}
	if got := store.Get("s"); len(got) != 2 || got[0].Content != "build" {
		t.Fatalf("store wrong: %+v", got)
	}
}

type fakeAsker struct{ answers [][]string }

func (f fakeAsker) Ask(context.Context, string, []question.Info) ([][]string, error) {
	return f.answers, nil
}

func TestQuestion_UsesQuestioner(t *testing.T) {
	ctx := tctx("")
	ctx.Questioner = fakeAsker{answers: [][]string{{"blue"}}}
	res, err := Question{}.Run(context.Background(), map[string]any{
		"questions": []any{map[string]any{
			"question": "color?", "header": "Color",
			"options": []any{map[string]any{"label": "red"}, map[string]any{"label": "blue"}},
		}},
	}, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Output, `"color?"="blue"`) {
		t.Fatalf("output = %q", res.Output)
	}
	if !strings.Contains(res.Title, "Asked 1 question") {
		t.Fatalf("title = %q", res.Title)
	}
}

func TestUnavailableTools_ErrorClearly(t *testing.T) {
	cases := map[string]Tool{
		"question":  Question{},
		"websearch": WebSearch{},
		"skill":     Skill{},
		"task":      Task{},
	}
	for name, tl := range cases {
		_, err := tl.Run(context.Background(), map[string]any{
			"text": "x", "query": "x", "name": "x", "description": "x", "prompt": "x",
		}, tctx(""))
		if err == nil {
			t.Fatalf("%s should error when dependency is unset", name)
		}
	}
}

type fakeRunner struct{ out string }

func (f fakeRunner) Run(context.Context, TaskRequest) (string, error) { return f.out, nil }

func TestTask_UsesRunner(t *testing.T) {
	res, err := Task{Runner: fakeRunner{out: "done"}}.Run(context.Background(),
		map[string]any{"description": "d", "prompt": "p", "agent": "build"}, tctx("ses_1"))
	if err != nil {
		t.Fatal(err)
	}
	if res.Output != "done" {
		t.Fatalf("task output = %q", res.Output)
	}
}

type errSearcher struct{}

func (errSearcher) Search(context.Context, string) (string, error) { return "", errors.New("boom") }

func TestWebSearch_PropagatesError(t *testing.T) {
	if _, err := (WebSearch{Searcher: errSearcher{}}).Run(context.Background(), map[string]any{"query": "x"}, tctx("")); err == nil {
		t.Fatal("expected searcher error")
	}
}
