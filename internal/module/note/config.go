package note

import "github.com/biairmal/go-sdk/lib/errorz"

// maxListSize is the largest page go-sdk's repository will return.
const maxListSize = 100

// Config is the modules.note block.
type Config struct {
	// ListMaxSize caps the size query parameter of GET /v1/notes.
	ListMaxSize int `mapstructure:"list_max_size"`
}

// Validate checks the block's own fields.
func (c Config) Validate() error {
	if c.ListMaxSize < 1 || c.ListMaxSize > maxListSize {
		return errorz.BadRequest().WithMessage("list_max_size must be between 1 and 100")
	}
	return nil
}
