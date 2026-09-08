//go:build windows

package codexprocess

import (
	"context"
	"os/exec"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const processCreationFlags = windows.CREATE_NEW_PROCESS_GROUP | windows.CREATE_SUSPENDED

func resumeInitialThread(processID uint32) error {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPTHREAD, 0)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(snapshot)
	entry := windows.ThreadEntry32{Size: uint32(unsafe.Sizeof(windows.ThreadEntry32{}))}
	if err := windows.Thread32First(snapshot, &entry); err != nil {
		return err
	}
	for {
		if entry.OwnerProcessID == processID {
			thread, err := windows.OpenThread(windows.THREAD_SUSPEND_RESUME, false, entry.ThreadID)
			if err != nil {
				return err
			}
			previous, resumeErr := windows.ResumeThread(thread)
			closeErr := windows.CloseHandle(thread)
			if resumeErr != nil {
				return resumeErr
			}
			if closeErr != nil {
				return closeErr
			}
			if previous == 0 {
				return syscall.EINVAL
			}
			return nil
		}
		if err := windows.Thread32Next(snapshot, &entry); err != nil {
			return err
		}
	}
}

func runCommand(ctx context.Context, command *exec.Cmd) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(job)
	limits := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	limits.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&limits)), uint32(unsafe.Sizeof(limits))); err != nil {
		return err
	}
	command.SysProcAttr = &syscall.SysProcAttr{CreationFlags: processCreationFlags}
	if err := command.Start(); err != nil {
		return err
	}
	processID := uint32(command.Process.Pid)
	process, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, processID)
	if err != nil {
		_ = command.Process.Kill()
		_ = command.Wait()
		return err
	}
	assignErr := windows.AssignProcessToJobObject(job, process)
	_ = windows.CloseHandle(process)
	if assignErr != nil {
		_ = command.Process.Kill()
		_ = command.Wait()
		return assignErr
	}
	if err := ctx.Err(); err != nil {
		_ = windows.TerminateJobObject(job, 1)
		_ = command.Wait()
		return err
	}
	if err := resumeInitialThread(processID); err != nil {
		_ = windows.TerminateJobObject(job, 1)
		_ = command.Wait()
		return err
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		_ = windows.TerminateJobObject(job, 1)
		<-done
		return ctx.Err()
	}
}
