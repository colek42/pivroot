package main

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	"github.com/containers/storage/pkg/reexec"
)

func init() {
	reexec.Register("bind_mount_and_run", bindMountAndRun)
	if reexec.Init() {
		os.Exit(0)
	}
}

func createDir(newRoot, dir string) error {
	err := os.MkdirAll(filepath.Join(newRoot, dir), 0755)
	if err != nil {
		return fmt.Errorf("failed to create %s directory: %v", dir, err)
	}
	return nil
}

func copyFile(src, dest string) error {
	err := exec.Command("cp", src, dest).Run()
	if err != nil {
		return fmt.Errorf("failed to copy %s to %s: %v", src, dest, err)
	}
	return nil
}

func createMinimalRootFs(newRoot string) error {
	dirs := []string{
		"bin",
		"cwd",
		"proc",
		"sys",
		"dev",
	}

	for _, dir := range dirs {
		if err := createDir(newRoot, dir); err != nil {
			return err
		}
	}

	if err := copyFile("/bin/busybox", filepath.Join(newRoot, "bin/busybox")); err != nil {
		return err
	}

	if err := exec.Command("cp", "-r", ".", filepath.Join(newRoot, "cwd")).Run(); err != nil {
		return fmt.Errorf("failed to copy current directory to new root: %v", err)
	}
	return nil
}

func mountProc(newRoot string) error {
	if err := syscall.Mount("proc", filepath.Join(newRoot, "proc"), "proc", 0, ""); err != nil {
		return fmt.Errorf("error mounting /proc: %v", err)
	}
	return nil
}

func chroot(newRoot string) error {
	if err := syscall.Chroot(newRoot); err != nil {
		return fmt.Errorf("error changing root: %v", err)
	}
	return nil
}

func changeDir(dir string) error {
	if err := os.Chdir(dir); err != nil {
		return fmt.Errorf("error changing to %s: %v", dir, err)
	}
	return nil
}

func bindMountAndRun() {
	newRoot := os.Getenv("NEW_ROOT")

	if err := mountProc(newRoot); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	if err := chroot(newRoot); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	if err := changeDir("/cwd"); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	cmd := exec.Command("/bin/busybox", "sh")
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = []string{"PATH=/bin"}

	err := cmd.Run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to run command in new user namespace with new filesystem: %v\n", err)
		os.Exit(1)
	}

	os.Exit(0)
}

func runInNewUserNamespaceWithNewFs() error {
	newRoot, err := ioutil.TempDir("", "new_root")
	if err != nil {
		return fmt.Errorf("failed to create temporary directory: %v", err)
	}
	defer os.RemoveAll(newRoot)

	if err := createMinimalRootFs(newRoot); err != nil {
		return err
	}

	cmd := reexec.Command("bind_mount_and_run")
	cmd.Env = append(os.Environ(), fmt.Sprintf("NEW_ROOT=%s", newRoot))
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	cmd.SysProcAttr = &syscall.SysProcAttr{
		Cloneflags: syscall.CLONE_NEWUSER | syscall.CLONE_NEWNS | syscall.CLONE_NEWPID,
		UidMappings: []syscall.SysProcIDMap{
			{
				ContainerID: 0,
				HostID:      os.Geteuid(),
				Size:        1,
			},
		},
		GidMappings: []syscall.SysProcIDMap{
			{
				ContainerID: 0,
				HostID:      os.Getegid(),
				Size:        1,
			},
		},
	}

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to run command in new user namespace with new filesystem: %v", err)
	}

	return nil
}

func main() {
	if err := runInNewUserNamespaceWithNewFs(); err != nil {
		fmt.Println("Error:", err)
	}
}
