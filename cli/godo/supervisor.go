package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

type supervisor interface {
	install(service service, binary string) error
	restart(service service) error
	remove(service service) error
}

type systemdSupervisor struct {
	unitDir string
}

func (s systemdSupervisor) install(service service, binary string) error {
	if _, err := exec.LookPath("systemctl"); err != nil {
		return errorsUnsupportedSupervisor()
	}
	unitFile := s.unitPath(service.ID)
	unit := renderUnit(service, binary)
	if err := writeAtomic(unitFile, []byte(unit), 0o644); err != nil {
		return fmt.Errorf("write user service: %w", err)
	}
	if err := systemctl("daemon-reload"); err != nil {
		_ = os.Remove(unitFile)
		return err
	}
	if err := systemctl("enable", "--now", s.unitName(service.ID)); err != nil {
		_ = systemctl("disable", "--now", s.unitName(service.ID))
		_ = os.Remove(filepath.Join(s.unitDir, "default.target.wants", s.unitName(service.ID)))
		_ = os.Remove(unitFile)
		_ = systemctl("daemon-reload")
		return err
	}
	return nil
}

func renderUnit(service service, binary string) string {
	return fmt.Sprintf(`[Unit]
Description=godo service %d: %s
After=network.target

[Service]
Type=simple
WorkingDirectory=%s
ExecStart=%s
Environment=%s
Restart=on-failure
RestartSec=2

[Install]
WantedBy=default.target
`, service.ID, unitDescription(service.Name), unitPath(service.WorkDir), unitQuote(binary), unitQuote("PORT="+strconv.Itoa(service.Port)))
}

func (s systemdSupervisor) restart(service service) error {
	return systemctl("restart", s.unitName(service.ID))
}

func (s systemdSupervisor) remove(service service) error {
	unit := s.unitName(service.ID)
	if err := systemctl("disable", "--now", unit); err != nil {
		return err
	}
	if err := os.Remove(s.unitPath(service.ID)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove user service: %w", err)
	}
	return systemctl("daemon-reload")
}

func (s systemdSupervisor) unitName(id int) string {
	return fmt.Sprintf("godo-%d.service", id)
}

func (s systemdSupervisor) unitPath(id int) string {
	return filepath.Join(s.unitDir, s.unitName(id))
}

func systemctl(arguments ...string) error {
	command := exec.Command("systemctl", append([]string{"--user"}, arguments...)...)
	var output bytes.Buffer
	command.Stdout = &output
	command.Stderr = &output
	if err := command.Run(); err != nil {
		message := strings.TrimSpace(output.String())
		if message == "" {
			message = err.Error()
		}
		return fmt.Errorf("systemctl --user %s: %s", strings.Join(arguments, " "), message)
	}
	return nil
}

func unitQuote(value string) string {
	value = strings.ReplaceAll(value, "%", "%%")
	value = strings.ReplaceAll(value, "\\", "\\\\")
	value = strings.ReplaceAll(value, "\"", "\\\"")
	return `"` + value + `"`
}

func unitPath(value string) string {
	var result strings.Builder
	for _, character := range []byte(value) {
		switch {
		case character == '%':
			result.WriteString("%%")
		case character == '/' || character == '.' || character == '_' || character == '-' ||
			character >= '0' && character <= '9' || character >= 'A' && character <= 'Z' || character >= 'a' && character <= 'z':
			result.WriteByte(character)
		default:
			fmt.Fprintf(&result, "\\x%02x", character)
		}
	}
	return result.String()
}

func unitDescription(value string) string {
	return strings.NewReplacer("\r", " ", "\n", " ").Replace(value)
}

func errorsUnsupportedSupervisor() error {
	return fmt.Errorf("systemd user services are required, but systemctl was not found")
}
