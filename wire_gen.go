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
	client := ProvideOpenAI(gptConfig)
	openAIClientV2 := ProvideOpenAIV2(gptConfig)
	assistantManager := &AssistantManager{
		OAI:    openAIClientV2,
		JSONDB: jsondb,
	}
	appDB := ProvideAppDB(jsondb)
	threadManager := &ThreadManager{
		OAI: openAIClientV2,
		db:  appDB,
	}
	runManager := &RunManager{
		oai: openAIClientV2,
		db:  appDB,
	}
	openAIConfig := ProvideOpenAIConfig(gptConfig)
	oaiClient := ProvideOAI(gptConfig)
	threadRunner := &ThreadRunner{
		OpenAIConfig: openAIConfig,
		OAI:          client,
		OAIV2:        openAIClientV2,
		AM:           assistantManager,
		oai:          oaiClient,
		appDB:        appDB,
		log:          logger,
	}
	app := &App{
		Args:             args,
		Config:           gptConfig,
		OAI:              client,
		AssistantManager: assistantManager,
		ThreadManager:    threadManager,
		RunManager:       runManager,
		ThreadRunner:     threadRunner,
	}
	return app, nil
}
