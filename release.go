package gauthor

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"os/exec"
	"strings"

	"github.com/Songmu/ghch"
	"github.com/Songmu/prompter"
	"github.com/motemen/gobump"
	"golang.org/x/xerrors"
)

type release struct {
	allowDirty, dryRun   bool
	branch, path         string
	outStream, errStream io.Writer
}

func (re *release) run(argv []string, outStream, errStream io.Writer) error {
	re.outStream = outStream
	re.errStream = errStream
	fs := flag.NewFlagSet("gauthor release", flag.ContinueOnError)
	fs.StringVar(&re.branch, "branch", "master", "releasing branch")
	fs.BoolVar(&re.allowDirty, "allow-dirty", false, "allow dirty index")
	fs.BoolVar(&re.dryRun, "dry-run", false, "dry run")

	fs.SetOutput(errStream)
	if err := fs.Parse(argv); err != nil {
		return err
	}
	re.path = fs.Arg(0)
	if re.path != "" {
		re.path = "."
	}
	return re.do()
}

type cmd struct {
	outStream, errStream io.Writer
	err                  error
}

func (c *cmd) git(args ...string) (string, string) {
	return c.run("git", args...)
}

func (c *cmd) run(prog string, args ...string) (string, string) {
	if c.err != nil {
		return "", ""
	}
	var (
		outBuf bytes.Buffer
		errBuf bytes.Buffer
	)
	cmd := exec.Command(prog, args...)
	cmd.Stdout = io.MultiWriter(&outBuf, c.outStream)
	cmd.Stderr = io.MultiWriter(&errBuf, c.errStream)
	c.err = cmd.Run()
	return outBuf.String(), errBuf.String()
}

func git(args ...string) (string, string, error) {
	var (
		outBuf bytes.Buffer
		errBuf bytes.Buffer
	)
	cmd := exec.Command("git", args...)
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err := cmd.Run()
	return outBuf.String(), errBuf.String(), err
}

func (re *release) do() error {
	if !re.allowDirty {
		out, _, err := git("status", "--porcelain")
		if err != nil {
			return xerrors.Errorf("faild to release when git status: %w", err)
		}
		if strings.TrimSpace(out) != "" {
			return xerrors.Errorf("can't release on dirty index (or you can use --allow-dirty)\n%s", out)
		}
	}

	buf := &bytes.Buffer{}
	gb := &gobump.Gobump{
		Show:      true,
		Raw:       true,
		OutStream: buf,
	}
	if _, err := gb.Run(); err != nil {
		return err
	}
	fmt.Fprintf(re.outStream, "current version: %s", buf.String())
	nextVer := prompter.Prompt("input next version", "")
	gb2 := &gobump.Gobump{
		Write: true,
		Config: gobump.Config{
			Exact: nextVer,
		},
	}
	filesMap, err := gb2.Run()
	if err != nil {
		return err
	}
	var versions []string
	for f := range filesMap {
		versions = append(versions, f)
	}

	fmt.Fprintf(re.outStream, "following changes will be released")
	gh := &ghch.Ghch{
		RepoPath:    ".",
		NextVersion: nextVer,
		Format:      "markdown",
		OutStream:   re.outStream,
	}
	if err := gh.Run(); err != nil {
		return err
	}
	gh.Write = true
	if err := gh.Run(); err != nil {
		return err
	}
	branch, _, _ := git("symbolic-ref", "--short", "HEAD")
	_ = branch

	c := &cmd{outStream: re.outStream, errStream: re.errStream}
	c.git(append([]string{"git", "CHANGELOG.md"}, versions...)...)
	c.git("commit", "-m",
		fmt.Sprintf("Checking in changes prior to tagging of version v%s", nextVer))
	c.git("tag", fmt.Sprintf("v%s", nextVer))
	// release branch should be specified? (default: master)
	// detect remote?
	c.git("push")
	c.git("push", "--tags")
	return c.err
}
