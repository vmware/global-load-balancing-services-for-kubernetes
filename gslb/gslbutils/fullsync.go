package gslbutils

import "time"

type FullSyncThread struct {
	Shutdown     chan interface{}
	Interval     time.Duration
	SyncFunction func()
}

func NewFullSyncThread(interval time.Duration) *FullSyncThread {
	return &FullSyncThread{
		Shutdown: make(chan interface{}),
		Interval: interval,
	}
}

func (t *FullSyncThread) Run() {
	ticker := time.NewTicker(t.Interval * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-t.Shutdown:
			return
		case _ = <-ticker.C:
			t.SyncFunction()
		}
	}
}
