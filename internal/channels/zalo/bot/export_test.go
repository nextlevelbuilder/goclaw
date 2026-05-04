package bot

func (c *Channel) BootstrapDroppedForTest() int64 { return c.bootstrapDroppedCount.Load() }
