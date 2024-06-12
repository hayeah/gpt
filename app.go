package gpt

type App struct {
	Args             *Args
	Config           *Config
	AssistantManager *AssistantManager
	ThreadManager    *ThreadManager
	RunManager       *RunManager
	ThreadRunner     *ThreadRunner
	// Migrate          *migrate.Migrate
}

func (a *App) Run() error {
	args := a.Args

	switch {
	case args.Assistant != nil:
		am := a.AssistantManager
		switch {
		case args.Assistant.Show != nil:
			cmd := args.Assistant.Show
			return am.Show(cmd.ID)
		case args.Assistant.Create != nil:
			cmd := args.Assistant.Create
			return am.Create(cmd.AssistantFile)
		case args.Assistant.List != nil:
			return am.List()
		case args.Assistant.Use != nil:
			cmd := args.Assistant.Use
			return am.Use(cmd.ID)
		default:
			curid, err := a.AssistantManager.CurrentAssistantID()
			if err != nil {
				return err
			}

			return am.Show(curid)
		}
	case args.Thread != nil:
		switch {
		case args.Thread.Show != nil:
			cmd := args.Thread.Show
			return a.ThreadManager.Show(cmd.ThreadID)
		case args.Thread.Messages != nil:
			return a.ThreadManager.Messages()
		case args.Thread.Use != nil:
			cmd := args.Thread.Use
			return a.ThreadManager.Use(cmd.ID)
		}
	case args.Send != nil:
		cmd := *args.Send
		// return a.ThreadRunner.RunStream(cmd)
		return a.ThreadRunner.RunStream(cmd)
	case args.Run != nil:
		switch {
		case args.Run.Show != nil:
			return a.RunManager.Show()
		case args.Run.ListSteps != nil:
			return a.RunManager.ListSteps()
		default:
			return a.RunManager.Show()
		}
	}

	return nil
}

type AppDB struct {
	jsondb *JSONDB
}

const (
	keyCurrentThread = "currentThread"
	keyCurrentRun    = "currentRun"
)

// CurrentThreadID retrieves the current thread ID.
func (d *AppDB) CurrentThreadID() (string, error) {
	return d.jsondb.GetString(keyCurrentThread)
}

// PutCurrentThreadID sets the current thread ID.
func (d *AppDB) PutCurrentThreadID(threadID string) error {
	return d.jsondb.Put(keyCurrentThread, threadID)
}

// CurrentRunID retrieves the current run ID.
func (d *AppDB) CurrentRunID() (string, error) {
	return d.jsondb.GetString(keyCurrentRun)
}

// PutCurrentRun sets the current run ID.
func (d *AppDB) PutCurrentRun(id string) error {
	return d.jsondb.Put(keyCurrentRun, id)
}
