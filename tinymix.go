package main

import (
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/spf13/pflag"

	"fmt"
)

var (
	tinymix  string
	mixers   string
	device   int
	controls []string
	values   []string
	left     bool
	right    bool
)

func main() {
	pflag.CommandLine.SortFlags = false
	pflag.StringVarP(&tinymix, "tinymix", "t", "tinymix", "Path for tinymix binary to execute.")
	pflag.IntVarP(&device, "device", "D", 0, "Use the given card #.")
	pflag.StringVarP(&mixers, "mixers", "m", "", "Name of mixer(s) that must match, delimited by a semicolon. Leave unset to try global apply.")
	pflag.StringSliceVarP(&controls, "controls", "c", nil, "Ordered comma delimited list of controls to alter.")
	pflag.StringSliceVarP(&values, "values", "v", nil, "Ordered comma delimited list of values to set.")
	pflag.BoolVarP(&left, "left", "l", false, "Automatically check for and set left channel controls.")
	pflag.BoolVarP(&right, "right", "r", false, "Automatically check for and set right channel controls.")
	pflag.Parse()

	if device < 0 {
		panic("Device must be 0 or greater")
	}

	if len(controls) != len(values) {
		panic("Need equal control and value count to set")
	}

	if len(controls) == 0 {
		panic("Must set control and value to be set")
	}

	t, err := NewTinymix(tinymix, mixers, device)
	if err != nil {
		panic(fmt.Sprintf("Unable to init tinymix! Error: %v", err))
	}

	t.Set(controls, values, left, right)
}

type Tinymix struct {
	tinymix string
	mixers  []string
	device  int

	headerMixer map[string]string
	headerTable []string
	args        []*TinymixArg
}

func NewTinymix(tinymix string, mixers string, device int) (*Tinymix, error) {
	t := new(Tinymix)
	t.tinymix = tinymix
	if mixers != "" {
		t.mixers = strings.Split(mixers, ";")
	}
	t.device = device
	t.headerMixer = make(map[string]string)
	t.args = make([]*TinymixArg, 0)
	return t, t.init()
}

func (t *Tinymix) init() error {
	out, err := t.run("-t")
	if err != nil {
		return fmt.Errorf("tinymix: failed to execute: %v", err)
	}
	lines := strings.Split(out, "\n")

	stage := 0 //mixer header, table header, table values
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		if line == "" {
			continue
		}
		switch stage {
		case 0:
			if strings.Contains(line, "\t") {
				i--
				stage++
				continue
			}
			kv := strings.Split(line, ":")
			if len(kv) != 2 {
				return fmt.Errorf("tinymix: failed to process mixer header: len(kv) == %d", len(kv))
			}
			key := kv[0]
			val := strings.TrimSpace(kv[1])
			val = strings.TrimPrefix(val, "'")
			val = strings.TrimSuffix(val, "'")
			if key == "Mixer name" && t.mixers != nil {
				allowed := false
				for j := 0; j < len(t.mixers); j++ {
					if val == t.mixers[j] {
						allowed = true
						break
					}
				}
				if !allowed {
					return fmt.Errorf("tinymix: mixer '%s' not allowed", val)
				}
			}
			t.headerMixer[key] = val
		case 1:
			t.headerTable = strings.Split(line, "\t")
			stage++
		case 2:
			arg, err := t.NewTinymixArg(line)
			if err != nil {
				return err
			}
			t.args = append(t.args, arg)
		}
	}
	return nil
}

func (t *Tinymix) GetArg(name string) *TinymixArg {
	for i := 0; i < len(t.args); i++ {
		if name == t.args[i].Name {
			return t.args[i]
		}
	}
	return nil
}

func (t *Tinymix) Set(controls, values []string, left, right bool) {
	for i := 0; i < len(controls); i++ {
		m := controls[i]
		l := "L " + m
		r := "R " + m

		if arg := t.GetArg(m); arg != nil {
			if err := arg.SetValue(values[i]); err != nil {
				os.Stderr.WriteString(fmt.Sprintf("Failed to set control '%s': %v\n", arg.Name, err))
			} else {
				fmt.Printf("Set control '%s' (%d): %v\n", arg.Name, arg.Control, values[i])
			}
		} else {
			os.Stderr.WriteString(fmt.Sprintf("Failed to find control '%s'\n", m))
		}
		if left {
			if arg := t.GetArg(l); arg != nil {
				if err := arg.SetValue(values[i]); err != nil {
					os.Stderr.WriteString(fmt.Sprintf("Failed to set control '%s': %v\n", arg.Name, err))
				} else {
					fmt.Printf("Set control '%s' (%d): %v\n", arg.Name, arg.Control, values[i])
				}
			} else {
				os.Stderr.WriteString(fmt.Sprintf("Failed to find control '%s'\n", l))
			}
		}
		if right {
			if arg := t.GetArg(r); arg != nil {
				if err := arg.SetValue(values[i]); err != nil {
					os.Stderr.WriteString(fmt.Sprintf("Failed to set control '%s': %v\n", arg.Name, err))
				} else {
					fmt.Printf("Set control '%s' (%d): %v\n", arg.Name, arg.Control, values[i])
				}
			} else {
				os.Stderr.WriteString(fmt.Sprintf("Failed to find control '%s'\n", r))
			}
		}
	}
}

func (t *Tinymix) run(args ...string) (string, error) {
	cmdArgs := make([]string, 0)
	cmdArgs = append(cmdArgs,
		"-D", t.DeviceString(),
	)
	if args != nil && len(args) > 0 {
		cmdArgs = append(cmdArgs, args...)
	}

	process := exec.Command(t.tinymix, cmdArgs...)
	stdoutPipe, err := process.StdoutPipe()
	if err != nil {
		return "", err
	}
	stderrPipe, err := process.StderrPipe()
	if err != nil {
		return "", err
	}
	if err := process.Start(); err != nil {
		return "", err
	}
	stdout, err := io.ReadAll(stdoutPipe)
	if err != nil {
		return "", err
	}
	if len(stdout) > 0 {
		stdout = stdout[:len(stdout)-1]
	}
	stderr, err := io.ReadAll(stderrPipe)
	if len(stderr) > 0 {
		stderr = stderr[:len(stderr)-1]
	}
	if err != nil {
		return "", err
	}
	err = process.Wait()

	if len(stderr) > 0 {
		errMsg := strings.TrimPrefix(string(stderr), "Error: ")
		return string(stdout), fmt.Errorf("%s", errMsg)
	}
	return string(stdout), err
}

type TinymixArg struct {
	*Tinymix

	Control int
	Type    string
	Length  int
	Name    string
	Value   string
}

func (t *Tinymix) NewTinymixArg(line string) (*TinymixArg, error) {
	a := new(TinymixArg)
	split := strings.Split(line, "\t")
	for i := 0; i < len(t.headerTable); i++ {
		switch t.headerTable[i] {
		case "ctl":
			val, err := strconv.Atoi(split[i])
			if err != nil {
				return nil, err
			}
			a.Control = val
		case "type":
			a.Type = split[i]
		case "num":
			val, err := strconv.Atoi(split[i])
			if err != nil {
				return nil, err
			}
			a.Length = val
		case "name":
			a.Name = split[i]
		case "value":
			a.Value = split[i]
		default:
			return nil, fmt.Errorf("tinymix: unknown table header: %s", t.headerTable[i])
		}
	}
	a.Tinymix = t
	return a, nil
}

func (a *TinymixArg) SetValue(set string) error {
	switch a.Type {
	case "BOOL":
		val := ""
		switch strings.ToLower(set) {
		case "1", "t", "true", "y", "yes", "enable", "enabled", "on":
			val = "1"
		case "0", "f", "false", "n", "no", "disable", "disabled", "off":
			val = "0"
		default:
			return fmt.Errorf("tinymix: control %d: unknown boolean '%s'", a.Control, set)
		}
		if _, err := a.run(a.ControlString(), val); err != nil {
			return fmt.Errorf("tinymix: control %d: failed to set '%s': %v", a.Control, set, err)
		}
	case "ENUM":
		line, err := a.run("-t", a.ControlString())
		if err != nil {
			return fmt.Errorf("tinymix: control %d: failed to read enums: %v", a.Control, err)
		}
		kv := strings.Split(line, ":\t")
		key := kv[0]
		val := kv[1]
		if a.Name != key {
			return fmt.Errorf("tinymix: control %d: expected name '%s' but got '%s'", a.Control, a.Name, key)
		}
		enums := strings.Split(val, "\t")
		enum := -1
		for i := 0; i < len(enums); i++ {
			test := strings.TrimPrefix(enums[i], ">")
			if set == test {
				enum = i
				break
			}
		}
		if enum < 0 {
			return fmt.Errorf("tinymix: control %d: unknown enum '%s'", a.Control, set)
		}
		if _, err := a.run(a.ControlString(), fmt.Sprintf("%d", enum)); err != nil {
			return fmt.Errorf("tinymix: control %d: failed to set '%s': %v", a.Control, set, err)
		}
	case "INT":
		_, err := strconv.Atoi(set)
		if err != nil {
			return fmt.Errorf("tinymix: control %d: input '%s' is not an integer", a.Control, set)
		}
		if _, err := a.run(a.ControlString(), set); err != nil {
			return fmt.Errorf("tinymix: control %d: failed to set %s: %v", a.Control, set, err)
		}
	case "BYTE":
		return fmt.Errorf("tinymix: control %d: setting bytes is not yet supported", a.Control)
	}
	a.Value = set
	return nil
}

func (t *Tinymix) Args() []*TinymixArg {
	return t.args
}

func (t *Tinymix) DeviceString() string {
	return fmt.Sprintf("%d", t.device)
}

func (a *TinymixArg) ControlString() string {
	return fmt.Sprintf("%d", a.Control)
}
