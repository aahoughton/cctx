package claude

import (
	"fmt"
	"os"
	"path/filepath"

	toml "github.com/pelletier/go-toml/v2"
)

// Config holds user configuration loaded from ~/.config/cctx/config.toml.
type Config struct {
	LLM LLMConfig `toml:"llm"`
}

// LoadConfig reads the config file from the standard location.
// Returns a zero Config (not an error) if the file doesn't exist.
// Prints a warning to stderr if the file exists but cannot be parsed.
func LoadConfig() Config {
	var cfg Config

	home, err := os.UserHomeDir()
	if err != nil {
		return cfg
	}

	path := filepath.Join(home, ".config", "cctx", "config.toml")
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg
	}

	if err := toml.Unmarshal(data, &cfg); err != nil {
		fmt.Fprintf(os.Stderr, "warning: %s: %v\n", path, err)
	}
	return cfg
}
