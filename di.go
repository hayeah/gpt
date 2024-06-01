package gpt

import (
	"embed"
	"errors"
	"fmt"
	"os"
	"path"
	"strings"
	"syscall"

	"github.com/go-resty/resty/v2"
	"github.com/google/wire"
	"github.com/hayeah/goo"
	"github.com/jmoiron/sqlx"
	"github.com/sashabaranov/go-openai"
	"golang.org/x/term"

	_ "github.com/golang-migrate/migrate/v4/database/sqlite3"
	_ "github.com/mattn/go-sqlite3"
)

// ProvideDB provides a JSONDB instance.
func ProvideJSONDB(db *sqlx.DB) *JSONDB {
	return &JSONDB{
		DB:        db,
		TableName: "keys",
	}
}

// readSecret prompts the user for the OpenAI API key without echoing input to the terminal
func readSecret(prompt string) ([]byte, error) {
	if !term.IsTerminal(syscall.Stdin) {
		return nil, errors.New("read secret: stdin is not a terminal")
	}

	fmt.Print(prompt)
	secret, err := term.ReadPassword(int(syscall.Stdin))
	fmt.Println() // Print a newline after secret input
	if err != nil {
		return nil, fmt.Errorf("read secret: %w", err)
	}

	return secret, nil
}

//go:embed migrations/*.sql
var migratefs embed.FS

func provideEmbeddedMigrateConfig() *goo.EmbeddedMigrateConfig {
	return &goo.EmbeddedMigrateConfig{
		FS:        migratefs,
		EmbedPath: "migrations",
	}
}

type appdir string

func provideAppDir() (appdir, error) {
	// basecfgdir, err := os.UserConfigDir()
	// if err != nil {
	// 	return "", err
	// }
	// dir := path.Join(basecfgdir, "github.com/hayeah/gpt")

	basecfgdir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := path.Join(basecfgdir, ".github.com/hayeah/gpt")

	err = os.MkdirAll(string(dir), 0755)
	if err != nil {
		return "", err
	}

	return appdir(dir), nil
}

// ProvideConfig loads the configuration from the environment.
func ProvideConfig(appdir appdir, migrate *goo.EmbbededMigrate, jsondb *JSONDB) (*Config, error) {
	cfg, err := goo.ParseConfig[Config]("")
	if errors.Is(err, goo.ErrNoConfig) {
		cfg = &Config{}
		err = nil
	}

	if cfg.AppDir == "" {
		cfg.AppDir = string(appdir)
	}

	if err != nil {
		return nil, err
	}

	if cfg.OpenAI.APIKey == "" {
		apiKey, err := jsondb.GetString("openai.secret")
		if err != nil {
			return nil, err
		}

		// if no key in db, prompt for key, then save it in db
		if apiKey == "" {
			// TODO check tty, before prompting

			secret, err := readSecret("Enter OpenAI API key: ")
			if err != nil {
				return nil, err
			}

			apiKey = strings.TrimSpace(string(secret))

			if apiKey == "" {
				return nil, errors.New("OpenAI API key is required")
			}

			// TODO do a ping test to verify the key

			if apiKey != "" {
				err = jsondb.Put("openai.secret", apiKey)
				if err != nil {
					return nil, err
				}
			}
		}

		cfg.OpenAI.APIKey = apiKey
	}

	return cfg, nil
}

func ProvideGooConfig(appdir appdir) (*goo.Config, error) {
	cfg, err := goo.ParseConfig[goo.Config]("")
	if errors.Is(err, goo.ErrNoConfig) {
		cfg = &goo.Config{}
		cfg.Logging = &goo.LoggerConfig{}
		err = nil
	}

	if cfg.Database == nil {
		cfg.Database = &goo.DatabaseConfig{}
		dbfile := path.Join(string(appdir), "gpt.sqlite3")

		cfg.Database.Dialect = "sqlite3"
		cfg.Database.DSN = dbfile
	}

	if err != nil {
		return nil, err
	}

	return cfg, nil
}

// ProvideArgs parses cli args
func ProvideArgs() (*Args, error) {
	return goo.ParseArgs[Args]()
}

func ProvideOpenAIConfig(cfg *Config) *OpenAIConfig {
	return &cfg.OpenAI
}

type OpenAIClientV2 struct {
	openai.Client
}

func ProvideOpenAIV2(cfg *Config) *OpenAIClientV2 {
	ocfg := openai.DefaultConfig(cfg.OpenAI.APIKey)
	ocfg.AssistantVersion = "v2"
	oa := openai.NewClientWithConfig(ocfg)

	c := OpenAIClientV2{*oa}

	return &c
}

func ProvideOpenAI(cfg *Config) *openai.Client {
	return openai.NewClient(cfg.OpenAI.APIKey)
}

func ProvideOAI(cfg *Config) *OAIClient {
	client := resty.New().SetDebug(true)
	// client.EnableTrace()
	return &OAIClient{OpenAIV2(client, cfg.OpenAI.APIKey)}
}

// ProvideAppDB provides an AppDB instance.
func ProvideAppDB(jsondb *JSONDB) *AppDB {
	return &AppDB{jsondb}
}

var wires = wire.NewSet(
	ProvideGooConfig,
	goo.Wires,
	ProvideConfig,
	ProvideArgs,
	ProvideOpenAI,
	ProvideOpenAIV2,
	ProvideOpenAIConfig,
	ProvideJSONDB,
	ProvideAppDB,
	ProvideOAI,
	provideEmbeddedMigrateConfig,
	provideAppDir,

	wire.Struct(new(ThreadManager), "*"),
	wire.Struct(new(ThreadRunner), "*"),
	wire.Struct(new(AssistantManager), "*"),
	wire.Struct(new(RunManager), "*"),
	wire.Struct(new(App), "*"),
)
