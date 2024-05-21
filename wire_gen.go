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
	config, err := ProvideConfig()
	if err != nil {
		return nil, err
	}
	client := ProvideOpenAI(config)
	openAIClientV2 := ProvideOpenAIV2(config)
	gooConfig := ProvideGooConfig(config)
	shutdownContext, err := goo.ProvideShutdownContext()
	if err != nil {
		return nil, err
	}
	logger, err := goo.ProvideZeroLogger(gooConfig, shutdownContext)
	if err != nil {
		return nil, err
	}
	db, err := goo.ProvideSQLX(gooConfig, shutdownContext, logger)
	if err != nil {
		return nil, err
	}
	jsondb := ProvideJSONDB(db)
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
	openAIConfig := ProvideOpenAIConfig(config)
	threadRunner := &ThreadRunner{
		OpenAIConfig: openAIConfig,
		OAI:          client,
		OAIV2:        openAIClientV2,
		AM:           assistantManager,
		appDB:        appDB,
	}
	migrate, err := goo.ProvideMigrate(gooConfig)
	if err != nil {
		return nil, err
	}
	app := &App{
		Args:             args,
		Config:           config,
		OAI:              client,
		AssistantManager: assistantManager,
		ThreadManager:    threadManager,
		RunManager:       runManager,
		ThreadRunner:     threadRunner,
		Migrate:          migrate,
	}
	return app, nil
}
