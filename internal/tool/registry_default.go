package tool

// NewDefaultRegistry returns a Registry with all built-in tools registered.
func NewDefaultRegistry() *Registry {
	r := NewRegistry()
	r.Register(readTool{})
	r.Register(lsTool{})
	r.Register(globTool{})
	r.Register(grepTool{})
	r.Register(writeTool{})
	r.Register(editTool{})
	r.Register(bashTool{})
	return r
}
