/*
fscli is an example command-line application that uses go-fsimpl to perform a
few basic filesystem operations.

It uses the [autofs] package to lookup the filesystem implementation for the
given base URL, so all URL schemes supported by this module can be used.

# Usage

	usage: fscli <flags> <command> ...
	flags:

	  -base-url string
	    	Base URL of the filesystem

	commands:

	  ls [DIR]
	    	List the files in DIR. Defaults to '.' (the current directory).
	  cat [FILE]...
	    	Concatenate FILE(s) to standard output.
	  stat [FILE]
	    	Print information about FILE to standard output.

# Examples

	$ fscli -base-url=git+ssh://git@github.com/git-fixtures/basic//json ls
	 -rw-r--r-- 212.7KiB 2022-08-21 11:10 long.json
	 -rw-r--r--     706B 2022-08-21 11:10 short.json

	$ fscli -base-url=file:///tmp cat test.txt test2.txt
	this is the content of test.txt
	this is the content of test2.txt

	$ fscli -base-url=https://example.com stat .
	.:
	    Size:         648B
	    Modified:     2019-10-17T07:18:26Z
	    Mode:         -rw-r--r--
	    Content-Type: text/html; charset=UTF-8

	$ fscli -base-url=file:///tmp stat test.txt
	test.txt:
	    Size:         32B
	    Modified:     2022-08-20T19:08:49Z
	    Mode:         -rw-r--r--
	    Content-Type: text/plain; charset=utf-8
*/
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/hairyhenderson/go-fsimpl"
	"github.com/hairyhenderson/go-fsimpl/autofs"
	"github.com/hairyhenderson/go-fsimpl/tracefs"
	"go.opentelemetry.io/otel"
)

type opts struct {
	baseURL       string
	enableTracing bool
	useOpen       bool
}

func parseFlags(args []string) (*opts, []string) {
	prog := args[0]

	fs := flag.NewFlagSet("root", flag.ExitOnError)
	fs.SetOutput(os.Stdout)
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), `usage: %s <flags> <command> ...
flags:
`, prog)
		fs.PrintDefaults()
		fmt.Fprint(fs.Output(), `
commands:
  ls [DIR]
    	List the files in DIR. Defaults to '.' (the current directory).
  cat [FILE]...
    	Concatenate FILE(s) to standard output.
  stat [FILE]
    	Print information about FILE to standard output.
`)
	}

	o := opts{}
	fs.StringVar(&o.baseURL, "base-url", "", "Base URL of the filesystem")
	fs.BoolVar(&o.enableTracing, "tracing", false, "Enable tracing with OTel")
	fs.BoolVar(&o.useOpen, "use-open", false, "Open first instead of using filesystem shortcut methods")

	_ = fs.Parse(args[1:])

	fsArgs := fs.Args()

	return &o, fsArgs
}

func main() {
	o, fsArgs := parseFlags(os.Args)
	if err := run(o, fsArgs); err != nil {
		slog.Error("exiting with error", slog.Any("err", err))
		os.Exit(1)
	}
}

func run(o *opts, fsArgs []string) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGQUIT, syscall.SIGTERM)
	defer stop()

	if len(fsArgs) == 0 {
		return errors.New("no command specified")
	}

	subCmd := fsArgs[0]

	fsys, err := autofs.Lookup(o.baseURL)
	if err != nil {
		return err
	}

	fsys = fsimpl.WithContextFS(ctx, fsys)

	if o.enableTracing {
		closer, err := initTracing(context.WithoutCancel(ctx))
		if err != nil {
			return fmt.Errorf("init trace exporter: %w", err)
		}

		defer func() { _ = closer(context.WithoutCancel(ctx)) }()

		ctx2, span := otel.Tracer("fscli").Start(ctx, subCmd)
		defer span.End()

		fsys, err = tracefs.New(ctx2, fsys)
		if err != nil {
			return fmt.Errorf("new tracefs: %w", err)
		}
	}

	return cmd(fsys, subCmd, o.useOpen, fsArgs)
}

func cmd(fsys fs.FS, subCmd string, useOpen bool, fsArgs []string) error {
	switch subCmd {
	case "ls":
		if len(fsArgs) == 1 {
			fsArgs = append(fsArgs, ".")
		}

		if useOpen {
			return openLs(fsys, fsArgs[1], os.Stdout)
		}

		return fsLs(fsys, fsArgs[1], os.Stdout)
	case "cat":
		if len(fsArgs) == 1 {
			return errors.New("no files specified")
		}

		return cat(fsys, fsArgs[1:], os.Stdout)
	case "stat":
		if len(fsArgs) == 1 {
			fsArgs = append(fsArgs, ".")
		}

		if useOpen {
			return openStat(fsys, fsArgs[1], os.Stdout)
		}

		return fsStat(fsys, fsArgs[1], os.Stdout)
	}

	return fmt.Errorf("unknown command: %s", subCmd)
}

func openLs(fsys fs.FS, dir string, w io.Writer) error {
	f, err := fsys.Open(dir)
	if err != nil {
		return err
	}
	defer f.Close()

	if d, ok := f.(fs.ReadDirFile); ok {
		des, err := d.ReadDir(-1)
		if err != nil {
			return err
		}

		return ls(des, w)
	}

	return fmt.Errorf("not a directory: %s", dir)
}

func fsLs(fsys fs.FS, dir string, w io.Writer) error {
	des, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return fmt.Errorf("%T: %w", fsys, err)
	}

	return ls(des, w)
}

func ls(des []fs.DirEntry, w io.Writer) error {
	tw := tabwriter.NewWriter(w, 0, 0, 1, ' ', tabwriter.AlignRight)
	defer tw.Flush()

	for _, d := range des {
		fi, err := d.Info()
		if err != nil {
			return err
		}

		sz := ""

		if fi.IsDir() {
			sz = ""
		} else {
			sz = formatSize(fi.Size())
		}

		mtime := fi.ModTime().Format("2006-01-02 15:04")
		fmt.Fprintf(tw, "%s\t%s\t%s\t %s\n", fi.Mode(), sz, mtime, d.Name())
	}

	return nil
}

func cat(fsys fs.FS, files []string, w io.Writer) error {
	for _, name := range files {
		f, err := fsys.Open(name)
		if err != nil {
			return err
		}

		if _, err := io.Copy(w, f); err != nil {
			_ = f.Close()

			return err
		}

		_ = f.Close()
	}

	return nil
}

func openStat(fsys fs.FS, name string, w io.Writer) error {
	f, err := fsys.Open(name)
	if err != nil {
		return err
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return err
	}

	return stat(fi, name, w)
}

func fsStat(fsys fs.FS, name string, w io.Writer) error {
	fi, err := fs.Stat(fsys, name)
	if err != nil {
		return err
	}

	return stat(fi, name, w)
}

func stat(fi fs.FileInfo, name string, w io.Writer) error {
	ct := fsimpl.ContentType(fi)

	fmt.Fprintf(w, `%s:
	Size:         %s
	Modified:     %s
	Mode:         %s
	Content-Type: %s
`, name, formatSize(fi.Size()), fi.ModTime().Format(time.RFC3339), fi.Mode(), ct)

	return nil
}

func formatSize(size int64) string {
	switch {
	case size <= 1024:
		return fmt.Sprintf("%dB", size)
	case size <= 1024*1024:
		return fmt.Sprintf("%.1fKiB", float64(size)/1024)
	case size <= 1024*1024*1024:
		return fmt.Sprintf("%.1fMiB", float64(size)/1024/1024)
	default:
		return fmt.Sprintf("%.1fGiB", float64(size)/1024/1024/1024)
	}
}
