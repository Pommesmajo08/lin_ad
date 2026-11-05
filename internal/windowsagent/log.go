package windowsagent

import (
	"os"
	"sync"
)

// RotatingLog bounds offline/retry diagnostics without buffering durable job data.
type RotatingLog struct {
	mu   sync.Mutex
	path string
	file *os.File
}

func OpenLog(path string) (*RotatingLog, error) {
	l := &RotatingLog{path: path}
	if e := l.open(); e != nil {
		return nil, e
	}
	return l, nil
}
func (l *RotatingLog) open() error {
	if _, e := os.Lstat(l.path); e == nil {
		if e = VerifyPrivate(l.path); e != nil {
			return e
		}
	} else if !os.IsNotExist(e) {
		return e
	}
	f, e := os.OpenFile(l.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	if e = ProtectNew(l.path); e != nil {
		f.Close()
		return e
	}
	l.file = f
	return nil
}
func (l *RotatingLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file == nil {
		if e := l.open(); e != nil {
			return 0, e
		}
	}
	info, e := l.file.Stat()
	if e != nil {
		return 0, e
	}
	if info.Size()+int64(len(p)) > 5*1024*1024 {
		if e = l.file.Close(); e != nil {
			return 0, e
		}
		l.file = nil
		if e = os.Remove(l.path + ".1"); e != nil && !os.IsNotExist(e) {
			return 0, e
		}
		if e = replaceFile(l.path, l.path+".1"); e != nil {
			return 0, e
		}
		if e = l.open(); e != nil {
			return 0, e
		}
	}
	return l.file.Write(p)
}
