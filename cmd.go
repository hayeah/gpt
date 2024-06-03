package gpt

// cli commands & args

type Args struct {
	Assistant *AssistantCmdScope `arg:"subcommand:assistant" help:"manage assistants"`
	Send      *SendCmdScope      `arg:"subcommand:send" help:"run a message in a thread"`
	Thread    *ThreadCmdScope    `arg:"subcommand:thread" help:"manage threads"`
	Run       *RunCmdScope       `arg:"subcommand:run" help:"manage runs"`
}

type SendCmdScope struct {
	Inputs         []string `arg:"positional,required"`
	ContinueThread bool     `arg:"--continue,-c" help:"run message using the current thread"`
	Tools          string   `arg:"--tools" help:"process tool use with the given command"`

	// TODO remove
	Message string
}

type ThreadMessagesCmd struct {
}

type ThreadUseCmd struct {
	ID string `arg:"positional,required"`
}

type ThreadCmdScope struct {
	Show     *ThreadShowCmd     `arg:"subcommand:show" help:"show current thread info"`
	Messages *ThreadMessagesCmd `arg:"subcommand:messages" help:"list messages of current thread"`
	Use      *ThreadUseCmd      `arg:"subcommand:use" help:"use thread"`
}

type ThreadShowCmd struct {
	ThreadID string `arg:"positional"`
}

type AssistantListCmd struct {
	// Remote      string `arg:"positional"`
}

type AssistantUseCmd struct {
	ID string `arg:"positional,required"`
}

type AssistantCreateCmd struct {
	AssistantFile string `arg:"positional,required"`
}

type AssistantShowCmd struct {
	ID string `arg:"positional"`
}

type AssistantCmdScope struct {
	Show   *AssistantShowCmd   `arg:"subcommand:show" help:"show assistant info"`
	List   *AssistantListCmd   `arg:"subcommand:ls" help:"list assistants"`
	Use    *AssistantUseCmd    `arg:"subcommand:use" help:"use assistant"`
	Create *AssistantCreateCmd `arg:"subcommand:create" help:"create assistant"`
}

type RunCmdScope struct {
	Show      *RunShowCmd      `arg:"subcommand:show" help:"show run info"`
	ListSteps *RunListStepsCmd `arg:"subcommand:steps" help:"show steps"`
}

type RunListStepsCmd struct {
}

type RunShowCmd struct {
	ID string `arg:"positional"`
}
