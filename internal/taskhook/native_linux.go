package taskhook

import (
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

var procRoot = "/proc"

func smallProcFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", ErrUnsupported
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, 4097))
	if err != nil || len(data) > 4096 {
		return "", ErrUnsupported
	}
	return string(data), nil
}

func platformProcess(pid int) (ProcessIdentity, error) {
	root := procRoot + "/" + strconv.Itoa(pid)
	var st unix.Stat_t
	if unix.Stat(root, &st) != nil {
		return ProcessIdentity{}, ErrUnsupported
	}
	data, err := smallProcFile(root + "/stat")
	if err != nil {
		return ProcessIdentity{}, err
	}
	end := strings.LastIndex(data, ") ")
	if end == -1 {
		return ProcessIdentity{}, ErrUnsupported
	}
	fields := strings.Fields(data[end+2:])
	if len(fields) < 20 {
		return ProcessIdentity{}, ErrUnsupported
	}
	parent, err := strconv.Atoi(fields[1])
	if err != nil {
		return ProcessIdentity{}, ErrUnsupported
	}
	start, err := strconv.ParseUint(fields[19], 10, 64)
	if err != nil {
		return ProcessIdentity{}, ErrUnsupported
	}
	boot, err := smallProcFile(procRoot + "/sys/kernel/random/boot_id")
	if err != nil || !uuid.MatchString(strings.TrimSpace(boot)) {
		return ProcessIdentity{}, ErrUnsupported
	}
	return ProcessIdentity{PID: pid, PPID: parent, UID: int(st.Uid),
		Birth: fmt.Sprintf("%s:%d", strings.TrimSpace(boot), start)}, nil
}

func platformFiles(ctx context.Context, pid int) ([]heldFile, error) {
	root := procRoot + "/" + strconv.Itoa(pid)
	var files []heldFile
	var st unix.Stat_t
	if unix.Stat(root+"/exe", &st) != nil {
		return nil, ErrUnsupported
	}
	files = append(files, heldFile{id: statIdentity(st), executable: true})
	dir, err := os.Open(root + "/fd")
	if err != nil {
		return nil, ErrUnsupported
	}
	defer func() { _ = dir.Close() }()
	names, err := dir.Readdirnames(MaxHeldFiles + 1)
	if (err != nil && err != io.EOF) || len(names) > MaxHeldFiles {
		return nil, ErrUnsupported
	}
	for _, name := range names {
		if ctx.Err() != nil {
			return nil, ErrUnsupported
		}
		fd, parseErr := strconv.Atoi(name)
		if parseErr != nil || fd < 0 {
			return nil, ErrUnsupported
		}
		path := root + "/fd/" + strconv.Itoa(fd)
		target, readErr := os.Readlink(path)
		if readErr != nil || unix.Stat(path, &st) != nil {
			return nil, ErrUnsupported
		}
		if uint32(st.Mode)&unix.S_IFMT == unix.S_IFREG {
			files = append(files, heldFile{path: target, id: statIdentity(st)})
		}
	}
	return files, nil
}
