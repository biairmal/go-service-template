package note

import (
	"github.com/biairmal/go-sdk/lib/authz"
	"github.com/biairmal/go-sdk/lib/sqlkit"
	"github.com/biairmal/go-sdk/lib/validator"

	"github.com/biairmal/go-service-template/internal/platform/httpx"
)

// Permission keys. Register them in identity's modules.rbac.permissions and
// grant them to roles there; identity can only grant keys in its catalog.
const (
	// PermRead lists and reads the caller's own notes.
	PermRead = "notes:read"
	// PermReadAny lists and reads every note in the tenant.
	PermReadAny = "notes:read:any"
	// PermWrite creates notes and changes or deletes the caller's own.
	PermWrite = "notes:write"
	// PermWriteAny changes or deletes any note in the tenant (with PermWrite).
	PermWriteAny = "notes:write:any"
)

// Deps are what note needs from internal/app.
type Deps struct {
	DB    *sqlkit.DB
	V     validator.Validator
	Authz authz.Authorizer
}

// Module is the note feature boundary.
type Module struct {
	h *Handler
}

// New builds repository → service → handler from the modules.note block.
func New(cfg Config, deps Deps) *Module {
	svc := NewService(cfg, NewRepository(deps.DB), deps.Authz)
	return &Module{h: NewHandler(svc, deps.V, deps.Authz)}
}

// Register mounts /v1/notes.
func (m *Module) Register(rt *httpx.Routes) {
	rt.API.Route("/notes", m.h.Register)
}
