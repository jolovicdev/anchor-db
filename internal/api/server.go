package api

import (
	"encoding/json"
	"html/template"
	"net/http"
	"strings"

	"anchordb/internal/app"
	"anchordb/internal/domain"
)

type Server struct {
	service *app.Service
	mux     *http.ServeMux
}

func NewServer(service *app.Service) http.Handler {
	server := &Server{
		service: service,
		mux:     http.NewServeMux(),
	}
	server.routes()
	return server.mux
}

func (s *Server) routes() {
	s.mux.HandleFunc("/", s.handleHome)
	s.mux.HandleFunc("/v1/repos", s.handleRepos)
	s.mux.HandleFunc("/v1/anchors", s.handleAnchors)
	s.mux.HandleFunc("/v1/anchors/", s.handleAnchorSubroutes)
	s.mux.HandleFunc("/v1/context", s.handleContext)
	s.mux.HandleFunc("/view", s.handleView)
}

func (s *Server) handleHome(w http.ResponseWriter, r *http.Request) {
	repos, err := s.service.ListRepos(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	type repoView struct {
		ID      string
		Name    string
		Path    string
		Anchors int
	}
	data := struct {
		Repos []repoView
	}{}
	for _, repo := range repos {
		anchors, err := s.service.ListAnchors(r.Context(), domain.AnchorFilter{RepoID: repo.ID})
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		data.Repos = append(data.Repos, repoView{
			ID:      repo.ID,
			Name:    repo.Name,
			Path:    repo.RootPath,
			Anchors: len(anchors),
		})
	}
	tmpl := template.Must(template.New("home").Parse(`<!doctype html><html><head><meta charset="utf-8"><title>AnchorDB</title><style>body{font-family:ui-sans-serif,system-ui,sans-serif;max-width:1080px;margin:40px auto;padding:0 16px}table{border-collapse:collapse;width:100%}th,td{padding:8px;border-bottom:1px solid #ddd;text-align:left}code{background:#f5f5f5;padding:2px 4px;border-radius:4px}a{text-decoration:none;color:#0b5ed7}</style></head><body><h1>AnchorDB</h1><p>Code memory for humans and agents.</p><table><thead><tr><th>Repo</th><th>Path</th><th>Anchors</th><th>Viewer</th></tr></thead><tbody>{{range .Repos}}<tr><td><code>{{.ID}}</code><br>{{.Name}}</td><td>{{.Path}}</td><td>{{.Anchors}}</td><td><a href="/view?repo_id={{.ID}}&ref=WORKTREE&path=sample.go">open sample.go</a></td></tr>{{else}}<tr><td colspan="4">No repos registered yet.</td></tr>{{end}}</tbody></table></body></html>`))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = tmpl.Execute(w, data)
}

func (s *Server) handleRepos(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		repos, err := s.service.ListRepos(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, repos)
	case http.MethodPost:
		var request struct {
			Name string `json:"name"`
			Path string `json:"path"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		repo, err := s.service.RegisterRepo(r.Context(), request.Name, request.Path)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, repo)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleAnchors(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		filter := domain.AnchorFilter{
			RepoID:     r.URL.Query().Get("repo_id"),
			Path:       r.URL.Query().Get("path"),
			SymbolPath: r.URL.Query().Get("symbol"),
		}
		if status := r.URL.Query().Get("status"); status != "" {
			filter.Status = domain.AnchorStatus(status)
		}
		anchors, err := s.service.ListAnchors(r.Context(), filter)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, anchors)
	case http.MethodPost:
		var request struct {
			RepoID    string   `json:"repo_id"`
			Ref       string   `json:"ref"`
			Path      string   `json:"path"`
			StartLine int      `json:"start_line"`
			StartCol  int      `json:"start_col"`
			EndLine   int      `json:"end_line"`
			EndCol    int      `json:"end_col"`
			Kind      string   `json:"kind"`
			Title     string   `json:"title"`
			Body      string   `json:"body"`
			Author    string   `json:"author"`
			Tags      []string `json:"tags"`
			Symbol    string   `json:"symbol"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		anchor, err := s.service.CreateAnchor(r.Context(), app.CreateAnchorInput{
			RepoID:    request.RepoID,
			Ref:       request.Ref,
			Path:      request.Path,
			StartLine: request.StartLine,
			StartCol:  request.StartCol,
			EndLine:   request.EndLine,
			EndCol:    request.EndCol,
			Kind:      request.Kind,
			Title:     request.Title,
			Body:      request.Body,
			Author:    request.Author,
			Tags:      request.Tags,
			Symbol:    request.Symbol,
		})
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, anchor)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleContext(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	result, err := s.service.Context(r.Context(), app.ContextRequest{
		RepoID: r.URL.Query().Get("repo_id"),
		Ref:    r.URL.Query().Get("ref"),
		Path:   r.URL.Query().Get("path"),
		Symbol: r.URL.Query().Get("symbol"),
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleAnchorSubroutes(w http.ResponseWriter, r *http.Request) {
	trimmed := strings.TrimPrefix(r.URL.Path, "/v1/anchors/")
	parts := strings.Split(strings.Trim(trimmed, "/"), "/")
	if len(parts) != 2 || parts[1] != "comments" {
		http.NotFound(w, r)
		return
	}
	anchorID := parts[0]
	switch r.Method {
	case http.MethodGet:
		comments, err := s.service.ListComments(r.Context(), anchorID)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, comments)
	case http.MethodPost:
		var request struct {
			ParentID string `json:"parent_id"`
			Author   string `json:"author"`
			Body     string `json:"body"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		comment, err := s.service.CreateComment(r.Context(), anchorID, request.ParentID, request.Author, request.Body)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, comment)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleView(w http.ResponseWriter, r *http.Request) {
	view, err := s.service.FileView(r.Context(), r.URL.Query().Get("repo_id"), r.URL.Query().Get("ref"), r.URL.Query().Get("path"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	tmpl := template.Must(template.New("view").Parse(`<!doctype html><html><head><meta charset="utf-8"><title>{{.Repo.Name}} - {{.Path}}</title><style>body{font-family:ui-sans-serif,system-ui,sans-serif;margin:0;background:#f6f7fb;color:#111}.layout{display:grid;grid-template-columns:260px 1fr 420px;min-height:100vh}.sidebar,.notes{padding:20px;background:#fff;border-right:1px solid #e4e7ec}.notes{border-right:none;border-left:1px solid #e4e7ec}.code{padding:20px;overflow:auto}.file-list a{display:block;padding:6px 8px;border-radius:6px;color:#0b5ed7;text-decoration:none}.file-list a.active{background:#e7f0ff;font-weight:600}.code-block{background:#111827;color:#e5e7eb;border-radius:10px;overflow:hidden}.code-line{display:grid;grid-template-columns:56px 1fr;gap:12px;padding:0 16px;white-space:pre;line-height:1.45}.code-line.highlighted{background:#312e81}.line-no{color:#9ca3af;padding:2px 0}.line-text{padding:2px 0}.anchor{border:1px solid #dbe2ea;background:#f8fafc;border-radius:10px;padding:12px;margin-bottom:12px}.comment{border-top:1px solid #e4e7ec;padding-top:8px;margin-top:8px}.meta{font-size:12px;color:#4b5563}.diff pre{background:#0f172a;color:#dbeafe;padding:12px;border-radius:10px;overflow:auto;white-space:pre-wrap}.section-title{margin-top:20px}</style></head><body><div class="layout"><aside class="sidebar"><h2>{{.Repo.Name}}</h2><p class="meta">{{.Ref}}</p><div class="file-list">{{range .Files}}<a class="{{if eq . $.Path}}active{{end}}" href="/view?repo_id={{$.Repo.ID}}&ref={{$.Ref}}&path={{.}}">{{.}}</a>{{end}}</div></aside><main class="code"><h1>{{.Path}}</h1><div class="code-block">{{range .Lines}}<div class="code-line {{if .Highlighted}}highlighted{{end}}"><span class="line-no">{{.Number}}</span><span class="line-text">{{if eq .Text ""}} {{else}}{{.Text}}{{end}}</span></div>{{end}}</div></main><aside class="notes"><h2>Anchors</h2>{{range .Anchors}}<div class="anchor"><strong>{{.Title}}</strong><div class="meta">{{.Kind}} · {{.Binding.SymbolPath}}</div><p>{{.Body}}</p>{{with index $.Comments .ID}}{{range .}}<div class="comment"><div class="meta">{{.Author}}</div><div>{{.Body}}</div></div>{{end}}{{end}}</div>{{else}}<p>No anchors for this file.</p>{{end}}<h2 class="section-title">Git Diff</h2><div class="diff">{{if .Diff}}<pre>{{.Diff}}</pre>{{else}}<p>No working tree diff for this file.</p>{{end}}</div></aside></div></body></html>`))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = tmpl.Execute(w, view)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
