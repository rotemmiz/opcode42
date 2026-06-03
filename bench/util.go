//go:build bench

package bench

import (
	"io"
	"os/exec"
	"strconv"
	"strings"
)

// killTree terminates the daemon process and any descendants it forked. opencode
// spawns helper processes; killing only the root would leave them holding the
// port and skewing the next iteration. We SIGKILL the root and each descendant.
func killTree(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	pid := cmd.Process.Pid
	// Collect descendants before killing the root (afterwards ppid links break).
	descendants := descendantPIDs(pid)
	_ = cmd.Process.Kill()
	for _, d := range descendants {
		_ = exec.Command("kill", "-9", strconv.Itoa(d)).Run()
	}
	_, _ = cmd.Process.Wait()
}

// descendantPIDs returns all transitive children of pid using `ps`.
func descendantPIDs(pid int) []int {
	out, err := exec.Command("ps", "-axo", "pid=,ppid=").Output()
	if err != nil {
		return nil
	}
	children := map[int][]int{}
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		if len(f) != 2 {
			continue
		}
		p, e1 := strconv.Atoi(f[0])
		pp, e2 := strconv.Atoi(f[1])
		if e1 != nil || e2 != nil {
			continue
		}
		children[pp] = append(children[pp], p)
	}
	var result []int
	seen := map[int]bool{}
	var walk func(int)
	walk = func(p int) {
		for _, c := range children[p] {
			if seen[c] {
				continue
			}
			seen[c] = true
			result = append(result, c)
			walk(c)
		}
	}
	walk(pid)
	return result
}

// drainAndDiscard drains r into the bit bucket so HTTP keep-alive can reuse the
// connection.
func drainAndDiscard(r io.Reader) (int64, error) {
	return io.Copy(io.Discard, r)
}

// execOutput runs bin with args and returns combined stdout as a string.
func execOutput(bin string, args ...string) (string, error) {
	out, err := exec.Command(bin, args...).Output()
	return string(out), err
}
