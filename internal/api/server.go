package api

import (
	"embed"
	"encoding/json"
	"html/template"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"anchordb/internal/app"
	"anchordb/internal/domain"
)

//go:embed templates/*.html
var templateFS embed.FS

type Server struct {
	service *app.Service
	mux     *http.ServeMux
	tmpl    *template.Template
}

type createRepoRequest struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

type createAnchorRequest struct {
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

type updateAnchorRequest struct {
	Kind   *string   `json:"kind"`
	Title  *string   `json:"title"`
	Body   *string   `json:"body"`
	Author *string   `json:"author"`
	Tags   *[]string `json:"tags"`
}

func NewServer(service *app.Service) http.Handler {
	tmpl := template.Must(template.ParseFS(templateFS, "templates/*.html"))
	server := &Server{
		service: service,
		mux:     http.NewServeMux(),
		tmpl:    tmpl,
	}
	server.routes()
	return server.withLogging(server.mux)
}

func (s *Server) routes() {
	s.mux.HandleFunc("/", s.handleHome)
	s.mux.HandleFunc("/health", s.handleHealth)
	s.mux.HandleFunc("/v1/repos", s.handleRepos)
	s.mux.HandleFunc("/v1/repos/", s.handleRepoSubroutes)
	s.mux.HandleFunc("/v1/anchors", s.handleAnchors)
	s.mux.HandleFunc("/v1/anchors/", s.handleAnchorSubroutes)
	s.mux.HandleFunc("/v1/context", s.handleContext)
	s.mux.HandleFunc("/v1/search", s.handleSearch)
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
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = s.tmpl.ExecuteTemplate(w, "home.html", data)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
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
		var request createRepoRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if strings.TrimSpace(request.Path) == "" {
			writeError(w, http.StatusBadRequest, "path is required")
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

func (s *Server) handleRepoSubroutes(w http.ResponseWriter, r *http.Request) {
	trimmed := strings.TrimPrefix(r.URL.Path, "/v1/repos/")
	parts := strings.Split(strings.Trim(trimmed, "/"), "/")
	if len(parts) == 1 {
		repoID := parts[0]
		switch r.Method {
		case http.MethodGet:
			repo, err := s.service.GetRepo(r.Context(), repoID)
			if err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, repo)
		case http.MethodDelete:
			if err := s.service.RemoveRepo(r.Context(), repoID); err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"deleted": true, "id": repoID})
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
		return
	}
	if len(parts) != 2 || parts[1] != "sync" || r.Method != http.MethodPost {
		http.NotFound(w, r)
		return
	}
	repo, err := s.service.SyncRepo(r.Context(), parts[0])
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, repo)
}

func (s *Server) handleAnchors(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		filter := domain.AnchorFilter{
			RepoID:     r.URL.Query().Get("repo_id"),
			Path:       r.URL.Query().Get("path"),
			SymbolPath: r.URL.Query().Get("symbol"),
		}
		limit, err := parseOptionalInt(r.URL.Query().Get("limit"))
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid limit")
			return
		}
		offset, err := parseOptionalInt(r.URL.Query().Get("offset"))
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid offset")
			return
		}
		filter.Limit = limit
		filter.Offset = offset
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
		var request createAnchorRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if err := validateAnchorRequest(request); err != nil {
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
	if strings.TrimSpace(r.URL.Query().Get("repo_id")) == "" {
		writeError(w, http.StatusBadRequest, "repo_id is required")
		return
	}
	if strings.TrimSpace(r.URL.Query().Get("path")) == "" {
		writeError(w, http.StatusBadRequest, "path is required")
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

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	queryText := strings.TrimSpace(r.URL.Query().Get("query"))
	if queryText == "" {
		writeError(w, http.StatusBadRequest, "query is required")
		return
	}
	limit, err := parseOptionalInt(r.URL.Query().Get("limit"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid limit")
		return
	}
	offset, err := parseOptionalInt(r.URL.Query().Get("offset"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid offset")
		return
	}
	searchQuery := domain.SearchQuery{
		Query:      queryText,
		RepoID:     r.URL.Query().Get("repo_id"),
		Path:       r.URL.Query().Get("path"),
		SymbolPath: r.URL.Query().Get("symbol"),
		Limit:      limit,
		Offset:     offset,
	}
	if kind := r.URL.Query().Get("kind"); kind != "" {
		searchQuery.Kind = domain.AnchorKind(kind)
	}
	hits, err := s.service.Search(r.Context(), searchQuery)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, hits)
}

func (s *Server) handleAnchorSubroutes(w http.ResponseWriter, r *http.Request) {
	trimmed := strings.TrimPrefix(r.URL.Path, "/v1/anchors/")
	parts := strings.Split(strings.Trim(trimmed, "/"), "/")
	if len(parts) == 1 {
		anchorID := parts[0]
		switch r.Method {
		case http.MethodGet:
			anchor, err := s.service.GetAnchor(r.Context(), anchorID)
			if err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, anchor)
		case http.MethodPatch:
			var request updateAnchorRequest
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
			if err := validateUpdateAnchorRequest(request); err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
			input := app.UpdateAnchorInput{ID: anchorID}
			if request.Kind != nil {
				input.Kind = strings.TrimSpace(*request.Kind)
			}
			if request.Title != nil {
				input.Title = strings.TrimSpace(*request.Title)
			}
			if request.Body != nil {
				input.Body = strings.TrimSpace(*request.Body)
			}
			if request.Author != nil {
				input.Author = strings.TrimSpace(*request.Author)
			}
			if request.Tags != nil {
				input.Tags = *request.Tags
				input.ReplaceTags = true
			}
			anchor, err := s.service.UpdateAnchor(r.Context(), input)
			if err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, anchor)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
		return
	}
	if len(parts) != 2 {
		http.NotFound(w, r)
		return
	}
	anchorID := parts[0]
	if parts[1] == "comments" {
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
			if strings.TrimSpace(request.Author) == "" {
				writeError(w, http.StatusBadRequest, "author is required")
				return
			}
			if strings.TrimSpace(request.Body) == "" {
				writeError(w, http.StatusBadRequest, "body is required")
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
		return
	}
	switch r.Method {
	case http.MethodPost:
		var (
			anchor domain.Anchor
			err    error
		)
		switch parts[1] {
		case "close":
			anchor, err = s.service.CloseAnchor(r.Context(), anchorID)
		case "reopen":
			anchor, err = s.service.ReopenAnchor(r.Context(), anchorID)
		case "resolve":
			anchor, err = s.service.ResolveAnchor(r.Context(), anchorID)
		default:
			http.NotFound(w, r)
			return
		}
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, anchor)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleView(w http.ResponseWriter, r *http.Request) {
	if strings.TrimSpace(r.URL.Query().Get("repo_id")) == "" {
		writeError(w, http.StatusBadRequest, "repo_id is required")
		return
	}
	if strings.TrimSpace(r.URL.Query().Get("path")) == "" {
		writeError(w, http.StatusBadRequest, "path is required")
		return
	}
	view, err := s.service.FileView(r.Context(), r.URL.Query().Get("repo_id"), r.URL.Query().Get("ref"), r.URL.Query().Get("path"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = s.tmpl.ExecuteTemplate(w, "view.html", view)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func parseOptionalInt(value string) (int, error) {
	if strings.TrimSpace(value) == "" {
		return 0, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, err
	}
	if parsed < 0 {
		return 0, httpError("must be >= 0")
	}
	return parsed, nil
}

func validateAnchorRequest(request createAnchorRequest) error {
	if strings.TrimSpace(request.RepoID) == "" {
		return httpError("repo_id is required")
	}
	if strings.TrimSpace(request.Path) == "" {
		return httpError("path is required")
	}
	if strings.TrimSpace(request.Kind) == "" {
		return httpError("kind is required")
	}
	if strings.TrimSpace(request.Title) == "" {
		return httpError("title is required")
	}
	if strings.TrimSpace(request.Body) == "" {
		return httpError("body is required")
	}
	if strings.TrimSpace(request.Author) == "" {
		return httpError("author is required")
	}
	if request.StartLine < 1 || request.StartCol < 1 || request.EndLine < 1 || request.EndCol < 1 {
		return httpError("line and column values must be positive")
	}
	if request.EndLine < request.StartLine {
		return httpError("end_line must be >= start_line")
	}
	if request.EndLine == request.StartLine && request.EndCol < request.StartCol {
		return httpError("end_col must be >= start_col when on the same line")
	}
	return validateKind(request.Kind)
}

func validateUpdateAnchorRequest(request updateAnchorRequest) error {
	if request.Kind != nil {
		if err := validateKind(strings.TrimSpace(*request.Kind)); err != nil {
			return err
		}
	}
	if request.Title != nil && strings.TrimSpace(*request.Title) == "" {
		return httpError("title must not be empty")
	}
	if request.Body != nil && strings.TrimSpace(*request.Body) == "" {
		return httpError("body must not be empty")
	}
	if request.Author != nil && strings.TrimSpace(*request.Author) == "" {
		return httpError("author must not be empty")
	}
	return nil
}

func validateKind(value string) error {
	switch value {
	case "warning", "todo", "handoff", "rationale", "invariant", "question":
		return nil
	default:
		return httpError("invalid kind")
	}
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (s *Server) withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		writer := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(writer, r)
		log.Printf("%s %s status=%d duration=%s", r.Method, r.URL.RequestURI(), writer.status, time.Since(start))
	})
}

type httpError string

func (e httpError) Error() string {
	return string(e)
}
