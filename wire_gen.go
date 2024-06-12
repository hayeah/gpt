// Code generated by Wire. DO NOT EDIT.

//go:generate go run -mod=mod github.com/google/wire/cmd/wire
//go:build !wireinject
// +build !wireinject

package gpt

import (
	"github.com/hayeah/goo"
)

import (
	_ "github.com/golang-migrate/migrate/v4/database/sqlite3"
	_ "github.com/mattn/go-sqlite3"
)

// Injectors from wire.go:

func InitApp() (*App, error) {
	args, err := ProvideArgs()
	if err != nil {
		return nil, err
	}
	gptAppdir, err := provideAppDir()
	if err != nil {
		return nil, err
	}
	embeddedMigrateConfig := provideEmbeddedMigrateConfig()
	config, err := ProvideGooConfig(gptAppdir)
	if err != nil {
		return nil, err
	}
	embbededMigrate, err := goo.ProvideEmbbededMigrate(embeddedMigrateConfig, config)
	if err != nil {
		return nil, err
	}
	logger, err := goo.ProvideSlog(config)
	if err != nil {
		return nil, err
	}
	shutdownContext, err := goo.ProvideShutdownContext(logger)
	if err != nil {
		return nil, err
	}
	db, err := goo.ProvideSQLX(config, shutdownContext, logger)
	if err != nil {
		return nil, err
	}
	jsondb := ProvideJSONDB(db)
	gptConfig, err := ProvideConfig(gptAppdir, embbededMigrate, jsondb)
	if err != nil {
		return nil, err
	}
	openAIV2API := ProvideOAI(gptConfig)
	assistantManager := &AssistantManager{
		oai:    openAIV2API,
		JSONDB: jsondb,
	}
	appDB := ProvideAppDB(jsondb)
	threadManager := &ThreadManager{
		db: appDB,
	}
	runManager := &RunManager{
		ai: openAIV2API,
		db: appDB,
	}
	threadRunner := &ThreadRunner{
		AM:    assistantManager,
		oai:   openAIV2API,
		appDB: appDB,
		log:   logger,
	}
	app := &App{
		Args:             args,
		Config:           gptConfig,
		AssistantManager: assistantManager,
		ThreadManager:    threadManager,
		RunManager:       runManager,
		ThreadRunner:     threadRunner,
	}
	return app, nil
}
