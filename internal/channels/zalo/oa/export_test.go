package oa

func (c *Channel) BootstrapDroppedForTest() int64 { return c.bootstrapDroppedCount.Load() }
