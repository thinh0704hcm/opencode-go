package tool

// NewDefaultRegistry returns a Registry with all built-in tools registered.
func NewDefaultRegistry() *Registry {
	r := NewRegistry()
	r.Register(readTool{})
	r.Register(lsTool{})
	r.Register(globTool{})
	r.Register(grepTool{})
	r.Register(webFetchTool{})
	r.Register(writeTool{})
	r.Register(editTool{})
	r.Register(bashTool{})
	r.Register(planSaveTool{})
	r.Register(planReadTool{})
	r.Register(worktreeCreateTool{})
	r.Register(worktreeDeleteTool{})
	r.Register(ptySpawnTool{})
	r.Register(ptyWriteTool{})
	r.Register(ptyReadTool{})
	r.Register(ptyListTool{})
	r.Register(ptyKillTool{})
	return r
}
