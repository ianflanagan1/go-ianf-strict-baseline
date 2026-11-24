package clockwrap

import "time"

func Read() string { return now().Format(time.RFC3339) }

func now() time.Time { return time.Now() }
