package taskhook

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

func platformProcess(pid int) (ProcessIdentity, error) {
	p, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil {
		return ProcessIdentity{}, ErrUnsupported
	}
	return ProcessIdentity{PID: int(p.Proc.P_pid), PPID: int(p.Eproc.Ppid), UID: int(p.Eproc.Ucred.Uid),
		Birth: fmt.Sprintf("darwin-%d:%d", p.Proc.P_starttime.Sec, p.Proc.P_starttime.Usec)}, nil
}

type limitedOutput struct{ bytes.Buffer }

func (b *limitedOutput) Write(p []byte) (int, error) {
	if b.Len()+len(p) > 256<<10 {
		return 0, ErrLimit
	}
	n, err := b.Buffer.Write(p)
	if err != nil {
		return n, ErrUnsupported
	}
	return n, nil
}

func platformFiles(ctx context.Context, pid int) ([]heldFile, error) {
	const script = `IFS= read -r pid || exit 1
case "$pid" in ''|*[!0-9]*) exit 1;; esac
exec /usr/sbin/lsof -a -p "$pid" -F0pfinD
`
	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", script)
	cmd.Stdin = strings.NewReader(strconv.Itoa(pid) + "\n")
	var output limitedOutput
	cmd.Stdout = &output
	cmd.Stderr = io.Discard
	if cmd.Run() != nil {
		return nil, ErrUnsupported
	}
	return parseLsof(output.String())
}

func parseLsof(data string) ([]heldFile, error) {
	var files []heldFile
	var path, device, inode, descriptor string
	flush := func() bool {
		if path == "" {
			return true
		}
		dev, err := strconv.ParseUint(strings.TrimPrefix(device, "0x"), 16, 64)
		if err != nil {
			return !strings.Contains(path, "/rollout-")
		}
		ino, err := strconv.ParseUint(inode, 10, 64)
		if err != nil {
			return !strings.Contains(path, "/rollout-")
		}
		files = append(files, heldFile{path: path, id: fmt.Sprintf("%x:%x", dev, ino), executable: descriptor == "txt"})
		return len(files) <= MaxHeldFiles
	}
	for _, field := range strings.Split(data, "\x00") {
		field = strings.TrimLeft(field, "\n")
		if len(field) == 0 {
			continue
		}
		switch field[0] {
		case 'f':
			if !flush() {
				return nil, ErrLimit
			}
			path = ""
			device = ""
			inode = ""
			descriptor = field[1:]
		case 'n':
			path = field[1:]
		case 'D':
			device = field[1:]
		case 'i':
			inode = field[1:]
		}
	}
	if !flush() {
		return nil, ErrLimit
	}
	return files, nil
}
