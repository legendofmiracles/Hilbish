package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

//	"github.com/bobappleyard/readline"
	"github.com/yuin/gopher-lua"
//	"github.com/yuin/gopher-lua/parse"
	"mvdan.cc/sh/v3/interp"
	"mvdan.cc/sh/v3/syntax"
)

func RunInput(input string) {
	cmdArgs, cmdString := splitInput(input)

	// If alias was found, use command alias
	for aliases[cmdArgs[0]] != "" {
		alias := aliases[cmdArgs[0]]
		cmdString = alias + strings.TrimPrefix(cmdString, cmdArgs[0])
		cmdArgs, cmdString = splitInput(cmdString)

		if aliases[cmdArgs[0]] == alias {
			break
		}
		if aliases[cmdArgs[0]] != "" {
			continue
		}
	}
	hooks.Em.Emit("command.preexec", input, cmdString)

	// First try to load input, essentially compiling to bytecode
	fn, err := l.LoadString(cmdString)
	if err != nil && noexecute {
		fmt.Println(err)
	/*	if lerr, ok := err.(*lua.ApiError); ok {
			if perr, ok := lerr.Cause.(*parse.Error); ok {
				print(perr.Pos.Line == parse.EOF)
			}
		}
	*/
		return
	}
	// And if there's no syntax errors and -n isnt provided, run
	if !noexecute {
		l.Push(fn)
		err = l.PCall(0, lua.MultRet, nil)
	}
	if err == nil {
		cmdFinish(0, cmdString)
		return
	}
	if commands[cmdArgs[0]] != nil {
		luacmdArgs := l.NewTable()
		for _, str := range cmdArgs[1:] {
			luacmdArgs.Append(lua.LString(str))
		}

		err := l.CallByParam(lua.P{
			Fn: commands[cmdArgs[0]],
			NRet:    1,
			Protect: true,
		}, luacmdArgs)
		if err != nil {
			fmt.Fprintln(os.Stderr,
				"Error in command:\n\n" + err.Error())
			cmdFinish(1, cmdString)
			return
		}
		luaexitcode := l.Get(-1)
		var exitcode uint8 = 0

		l.Pop(1)

		if code, ok := luaexitcode.(lua.LNumber); luaexitcode != lua.LNil && ok {
			exitcode = uint8(code)
		}

		cmdFinish(exitcode, cmdString)
		return
	}

	// Last option: use sh interpreter
	err = execCommand(cmdString)
	if err != nil {
		// If input is incomplete, start multiline prompting
		if syntax.IsIncomplete(err) {
			for {
				cmdString, err = ContinuePrompt(strings.TrimSuffix(cmdString, "\\"))
				if err != nil {
					break
				}
				err = execCommand(cmdString)
				if syntax.IsIncomplete(err) || strings.HasSuffix(input, "\\") {
					continue
				} else if code, ok := interp.IsExitStatus(err); ok {
					cmdFinish(code, cmdString)
				} else if err != nil {
					fmt.Fprintln(os.Stderr, err)
					cmdFinish(1, cmdString)
				}
				break
			}
		} else {
			if code, ok := interp.IsExitStatus(err); ok {
				cmdFinish(code, cmdString)
			} else {
				fmt.Fprintln(os.Stderr, err)
			}
		}
	} else {
		cmdFinish(0, cmdString)
	}
}

// Run command in sh interpreter
func execCommand(cmd string) error {
	file, err := syntax.NewParser().Parse(strings.NewReader(cmd), "")
	if err != nil {
		return err
	}

	exechandle := func(ctx context.Context, args []string) error {
		hc := interp.HandlerCtx(ctx)
		_, argstring := splitInput(strings.Join(args, " "))

		// If alias was found, use command alias
		for aliases[args[0]] != "" {
			alias := aliases[args[0]]
			argstring = alias + strings.TrimPrefix(argstring, args[0])
			cmdArgs, _ := splitInput(argstring)
			args = cmdArgs

			if aliases[args[0]] == alias {
				break
			}
			if aliases[args[0]] != "" {
				continue
			}
		}

		// If command is defined in Lua then run it
		luacmdArgs := l.NewTable()
		for _, str := range args[1:] {
			luacmdArgs.Append(lua.LString(str))
		}

		if commands[args[0]] != nil {
			err := l.CallByParam(lua.P{
				Fn: commands[args[0]],
				NRet:    1,
				Protect: true,
			}, luacmdArgs)
			luaexitcode := l.Get(-1)
			var exitcode uint8 = 0

			l.Pop(1)

			if code, ok := luaexitcode.(lua.LNumber); luaexitcode != lua.LNil && ok {
				exitcode = uint8(code)
			}

			if err != nil {
				fmt.Fprintln(os.Stderr,
					"Error in command:\n\n" + err.Error())
			}
			cmdFinish(exitcode, argstring)
			return interp.NewExitStatus(exitcode)
		}

		if _, err := interp.LookPathDir(hc.Dir, hc.Env, args[0]); err != nil {
			hooks.Em.Emit("command.not-found", args[0])
			return interp.NewExitStatus(127)
		}

		return interp.DefaultExecHandler(2 * time.Second)(ctx, args)
	}
	runner, _ := interp.New(
		interp.StdIO(os.Stdin, os.Stdout, os.Stderr),
		interp.ExecHandler(exechandle),
	)
	err = runner.Run(context.TODO(), file)

	return err
}

func splitInput(input string) ([]string, string) {
	// end my suffering
	// TODO: refactor this garbage
	quoted := false
	startlastcmd := false
	lastcmddone := false
	cmdArgs := []string{}
	sb := &strings.Builder{}
	cmdstr := &strings.Builder{}
	lastcmd := "" //readline.GetHistory(readline.HistorySize() - 1)

	for _, r := range input {
		if r == '"' {
			// start quoted input
			// this determines if other runes are replaced
			quoted = !quoted
			// dont add back quotes
			//sb.WriteRune(r)
		} else if !quoted && r == '~' {
			// if not in quotes and ~ is found then make it $HOME
			sb.WriteString(os.Getenv("HOME"))
		} else if !quoted && r == ' ' {
			// if not quoted and there's a space then add to cmdargs
			cmdArgs = append(cmdArgs, sb.String())
			sb.Reset()
		} else if !quoted && r == '^' && startlastcmd && !lastcmddone {
			// if ^ is found, isnt in quotes and is
			// the second occurence of the character and is
			// the first time "^^" has been used
			cmdstr.WriteString(lastcmd)
			sb.WriteString(lastcmd)

			startlastcmd = !startlastcmd
			lastcmddone = !lastcmddone

			continue
		} else if !quoted && r == '^' && !lastcmddone {
			// if ^ is found, isnt in quotes and is the
			// first time of starting "^^"
			startlastcmd = !startlastcmd
			continue
		} else {
			sb.WriteRune(r)
		}
		cmdstr.WriteRune(r)
	}
	if sb.Len() > 0 {
		cmdArgs = append(cmdArgs, sb.String())
	}

	return cmdArgs, cmdstr.String()
}

func cmdFinish(code uint8, cmdstr string) {
	hooks.Em.Emit("command.exit", code, cmdstr)
}
