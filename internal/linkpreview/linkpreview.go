// Package linkpreview serves the web app's index.html with per-post and
// per-profile link preview tags (AGORA-412).
//
// The web app is a single-page app, so every page used to carry the same
// title, description and Open Graph tags. A link pasted into iMessage, Slack,
// Discord, Bluesky or Mastodon showed a generic "Agora" card. Those apps read
// the tags from the HTML and never run the page's JavaScript, so the tags have
// to be in the HTML the server returns.
//
// nginx sends /post/{id} and /profile/{username} page loads here. The handler
// fetches index.html from the frontend container, swaps in tags describing the
// post or profile, and returns it, so the page then boots exactly as before.
// If anything fails it returns an error status and nginx falls back to
// serving the app straight from the frontend.
package linkpreview

import (
	"context"
	"database/sql"
	"fmt"
	"html"
	"io"
	"log"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"

	"github.com/agora-social/agora/internal/config"
	"github.com/agora-social/agora/internal/store"
)

// indexTTL bounds how long a fetched index.html is reused. Kept short because
// the file names the hashed asset bundles of the current build, so a stale
// copy would point browsers at bundles a redeploy has removed.
const indexTTL = 15 * time.Second

// descriptionLimit is roughly what preview cards show before cutting off.
const descriptionLimit = 200

type Service struct {
	db     *store.DB
	cfg    *config.Config
	client *http.Client

	mu        sync.Mutex
	index     string
	fetchedAt time.Time
}

func NewService(db *store.DB, cfg *config.Config) *Service {
	return &Service{db: db, cfg: cfg, client: &http.Client{Timeout: 5 * time.Second}}
}

func RegisterRoutes(r chi.Router, s *Service) {
	for _, m := range []string{http.MethodGet, http.MethodHead} {
		r.MethodFunc(m, "/linkpreview/post/{id}", s.Post)
		r.MethodFunc(m, "/linkpreview/profile/{username}", s.Profile)
	}
}

// Meta is the set of tags written into a page.
type Meta struct {
	Title       string
	Description string
	Image       string
	URL         string
	Type        string // og:type
	// LargeImage is set when Image is the post's own photo or video frame,
	// which earns the large card; an avatar or the instance logo stays small.
	LargeImage bool
}

func (s *Service) Post(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	meta, ok := s.postMeta(r.Context(), id)
	if !ok {
		meta = s.defaultMeta(r.URL.Path)
	}
	s.serve(w, r, meta)
}

func (s *Service) Profile(w http.ResponseWriter, r *http.Request) {
	username := chi.URLParam(r, "username")
	meta, ok := s.profileMeta(r.Context(), username)
	if !ok {
		meta = s.defaultMeta(r.URL.Path)
	}
	s.serve(w, r, meta)
}

func (s *Service) serve(w http.ResponseWriter, r *http.Request, meta Meta) {
	page, err := s.indexHTML(r.Context())
	if err != nil {
		// nginx serves the plain app from the frontend on any error status.
		log.Printf("linkpreview: fetching index.html: %v", err)
		http.Error(w, "index unavailable", http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// The page names the current build's bundles, so it must not be cached
	// past a redeploy, same as the frontend's own index.html.
	w.Header().Set("Cache-Control", "no-cache")
	if r.Method == http.MethodHead {
		return
	}
	if meta.Image == "" {
		meta.Image = s.absolute(s.logoURL())
	}
	io.WriteString(w, InjectMeta(page, meta, s.instanceName()))
}

// logoURL is the admin's uploaded instance logo, else the stock one the app
// ships. Preview scrapers need it absolute, which serve takes care of.
func (s *Service) logoURL() string {
	var logo string
	s.db.QueryRow(`SELECT value FROM instance_settings WHERE key = 'logo_url' AND value != ''`).Scan(&logo)
	if logo == "" {
		logo = "/logo-circle.svg"
	}
	return logo
}

func (s *Service) indexHTML(ctx context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.index != "" && time.Since(s.fetchedAt) < indexTTL {
		return s.index, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(s.cfg.FrontendInternalURL, "/")+"/index.html", nil)
	if err != nil {
		return "", err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("frontend answered %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	if !strings.Contains(string(body), "</head>") {
		return "", fmt.Errorf("frontend index.html has no </head>")
	}
	s.index, s.fetchedAt = string(body), time.Now()
	return s.index, nil
}

// postMeta describes a post only when a signed-out visitor could open it:
// public, top level, not deleted, not in a group, and not by a private,
// suspended or departing account. Anything else gets the generic tags, so a
// preview never reveals more than the page itself would.
func (s *Service) postMeta(ctx context.Context, id string) (Meta, bool) {
	var (
		content, cw, imageURL, videoThumb, photo string
		username, displayName, avatar            string
		isRemote                                 bool
	)
	err := s.db.QueryRowContext(ctx, `
		SELECT COALESCE(p.content,''), COALESCE(p.content_warning,''),
		       COALESCE(p.image_url,''), COALESCE(p.video_thumb_url,''),
		       COALESCE((SELECT ph.url FROM post_photos ph WHERE ph.post_id = p.id ORDER BY ph.position LIMIT 1), ''),
		       u.username, COALESCE(u.display_name,''), COALESCE(u.avatar_url,''), u.is_remote
		FROM posts p
		JOIN users u ON u.id = p.author_id
		WHERE p.id = $1
		  AND p.deleted_at IS NULL
		  AND p.parent_id IS NULL
		  AND p.community_group_id IS NULL
		  AND p.visibility = 'public'
		  AND NOT u.profile_private
		  AND NOT u.is_suspended
		  AND u.deletion_scheduled_at IS NULL
	`, id).Scan(&content, &cw, &imageURL, &videoThumb, &photo, &username, &displayName, &avatar, &isRemote)
	if err != nil {
		if err != sql.ErrNoRows {
			log.Printf("linkpreview: post %s: %v", id, err)
		}
		return Meta{}, false
	}

	m := Meta{
		Title: postTitle(displayName, s.handle(username, isRemote)),
		URL:   s.absolute("/post/" + id),
		Type:  "article",
	}
	if cw != "" {
		// The warning is the point: the preview must not show what it covers.
		m.Description = "Content warning: " + cw
		m.Image = s.absolute(avatar)
		return m, true
	}
	m.Description = Summarize(content, descriptionLimit)
	for _, img := range []string{photo, imageURL, videoThumb} {
		if img != "" {
			m.Image, m.LargeImage = s.absolute(img), true
			break
		}
	}
	if m.Image == "" {
		m.Image = s.absolute(avatar)
	}
	return m, true
}

func (s *Service) profileMeta(ctx context.Context, username string) (Meta, bool) {
	var displayName, bio, avatar string
	var isRemote bool
	err := s.db.QueryRowContext(ctx, `
		SELECT COALESCE(display_name,''), COALESCE(bio,''), COALESCE(avatar_url,''), is_remote
		FROM users
		WHERE username = $1
		  AND NOT profile_private
		  AND NOT is_suspended
		  AND deletion_scheduled_at IS NULL
	`, username).Scan(&displayName, &bio, &avatar, &isRemote)
	if err != nil {
		if err != sql.ErrNoRows {
			log.Printf("linkpreview: profile %s: %v", username, err)
		}
		return Meta{}, false
	}
	handle := s.handle(username, isRemote)
	title := handle
	if displayName != "" {
		title = displayName + " (" + handle + ")"
	}
	return Meta{
		Title:       title,
		Description: Summarize(bio, descriptionLimit),
		Image:       s.absolute(avatar),
		URL:         s.absolute("/profile/" + username),
		Type:        "profile",
	}, true
}

// defaultMeta is used for anything not public: it only names the instance, so
// the page's own access rules decide what a visitor sees.
func (s *Service) defaultMeta(path string) Meta {
	return Meta{URL: s.absolute(path), Type: "website"}
}

// handle formats a username the way the app displays it: remote stubs already
// carry user@domain (Bluesky handles are self-contained), and local accounts
// gain this instance's host.
func (s *Service) handle(username string, isRemote bool) string {
	if isRemote {
		return "@" + username
	}
	host := strings.TrimPrefix(strings.TrimPrefix(s.cfg.InstanceDomain, "https://"), "http://")
	return "@" + username + "@" + strings.TrimRight(host, "/")
}

func (s *Service) absolute(u string) string {
	if u == "" || strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://") {
		return u
	}
	return strings.TrimRight(s.cfg.InstanceDomain, "/") + "/" + strings.TrimLeft(u, "/")
}

func (s *Service) instanceName() string {
	var name string
	s.db.QueryRow(`SELECT value FROM instance_settings WHERE key = 'instance_name'`).Scan(&name)
	if name == "" {
		name = s.cfg.InstanceName
	}
	return name
}

func postTitle(displayName, handle string) string {
	if displayName == "" {
		return handle
	}
	return displayName + " (" + handle + ")"
}

var (
	tagRe        = regexp.MustCompile(`<[^>]*>`)
	spaceRe      = regexp.MustCompile(`\s+`)
	replacedTags = regexp.MustCompile(`(?is)<title>.*?</title>|<meta\s+(?:property="og:[^"]*"|name="twitter:[^"]*"|name="description")[^>]*>\s*`)
)

// Summarize flattens post or bio text to one line and trims it to about
// limit characters, at a word boundary where there is one.
func Summarize(text string, limit int) string {
	t := html.UnescapeString(tagRe.ReplaceAllString(text, " "))
	t = strings.TrimSpace(spaceRe.ReplaceAllString(t, " "))
	if utf8.RuneCountInString(t) <= limit {
		return t
	}
	r := []rune(t)[:limit]
	cut := string(r)
	if i := strings.LastIndex(cut, " "); i > limit/2 {
		cut = cut[:i]
	}
	return strings.TrimRight(cut, " .,;:") + "…"
}

// InjectMeta removes the page's own title, description, Open Graph and
// Twitter tags and writes ones for m in their place. An empty field keeps
// the instance-wide default for it, so the generic page still names the
// instance rather than going blank.
func InjectMeta(page string, m Meta, instanceName string) string {
	if instanceName == "" {
		instanceName = "Agora"
	}
	title := instanceName
	if m.Title != "" {
		title = m.Title + " on " + instanceName
	}
	desc := m.Description
	if desc == "" && m.Title == "" {
		desc = "A private, federated social network for the people you actually know."
	}
	ogType := m.Type
	if ogType == "" {
		ogType = "website"
	}
	card := "summary"
	if m.Image != "" && m.LargeImage {
		card = "summary_large_image"
	}

	esc := html.EscapeString
	var b strings.Builder
	fmt.Fprintf(&b, "<title>%s</title>\n", esc(title))
	if desc != "" {
		fmt.Fprintf(&b, "    <meta name=\"description\" content=\"%s\" />\n", esc(desc))
	}
	fmt.Fprintf(&b, "    <meta property=\"og:site_name\" content=\"%s\" />\n", esc(instanceName))
	fmt.Fprintf(&b, "    <meta property=\"og:type\" content=\"%s\" />\n", esc(ogType))
	fmt.Fprintf(&b, "    <meta property=\"og:title\" content=\"%s\" />\n", esc(firstNonEmpty(m.Title, instanceName)))
	if desc != "" {
		fmt.Fprintf(&b, "    <meta property=\"og:description\" content=\"%s\" />\n", esc(desc))
	}
	if m.URL != "" {
		fmt.Fprintf(&b, "    <meta property=\"og:url\" content=\"%s\" />\n", esc(m.URL))
	}
	if m.Image != "" {
		fmt.Fprintf(&b, "    <meta property=\"og:image\" content=\"%s\" />\n", esc(m.Image))
		fmt.Fprintf(&b, "    <meta name=\"twitter:image\" content=\"%s\" />\n", esc(m.Image))
	}
	fmt.Fprintf(&b, "    <meta name=\"twitter:card\" content=\"%s\" />\n", card)
	fmt.Fprintf(&b, "    <meta name=\"twitter:title\" content=\"%s\" />\n", esc(firstNonEmpty(m.Title, instanceName)))
	if desc != "" {
		fmt.Fprintf(&b, "    <meta name=\"twitter:description\" content=\"%s\" />\n", esc(desc))
	}

	stripped := replacedTags.ReplaceAllString(page, "")
	// Keep the logo image when nothing more specific was found, by leaving
	// the page's own og:image out of the strip only in that case.
	if m.Image == "" {
		if img := ogImageRe.FindString(page); img != "" {
			b.WriteString("    " + img + "\n")
		}
	}
	return strings.Replace(stripped, "</head>", "    "+b.String()+"  </head>", 1)
}

var ogImageRe = regexp.MustCompile(`<meta\s+property="og:image"[^>]*>`)

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
