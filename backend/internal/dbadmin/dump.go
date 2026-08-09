package dbadmin

import (
	"context"
	"fmt"
	"strings"
)

// MaxArchiveBytes caps Import/Export payloads for v1.
const MaxArchiveBytes = 32 << 20 // 32 MiB

// ExecFunc runs a command inside a database pod. Provided by the kubernetes client.
type ExecFunc func(ctx context.Context, command []string, stdin []byte) (stdout, stderr []byte, err error)

// DumpArchive runs the engine dump tool and returns the archive bytes.
func DumpArchive(ctx context.Context, engine Engine, creds Credentials, exec ExecFunc) ([]byte, string, string, error) {
	spec, ok := SpecFor(engine)
	if !ok {
		return nil, "", "", fmt.Errorf("unsupported engine %q", engine)
	}
	if exec == nil {
		return nil, "", "", fmt.Errorf("exec function required")
	}

	cmd, contentType, err := dumpCommand(engine, creds)
	if err != nil {
		return nil, "", "", err
	}

	stdout, stderr, err := exec(ctx, cmd, nil)
	if err != nil {
		return nil, "", "", fmt.Errorf("%s failed: %w (%s)", spec.DumpTool, err, strings.TrimSpace(string(stderr)))
	}
	if len(stdout) == 0 {
		return nil, "", "", fmt.Errorf("%s produced an empty archive", spec.DumpTool)
	}
	if len(stdout) > MaxArchiveBytes {
		return nil, "", "", fmt.Errorf("archive is %d bytes; limit is %d — export a smaller database", len(stdout), MaxArchiveBytes)
	}

	filename := "database." + spec.ArchiveExt
	return stdout, filename, contentType, nil
}

// RestoreArchive feeds archive bytes into the engine restore tool.
func RestoreArchive(ctx context.Context, engine Engine, creds Credentials, archive []byte, exec ExecFunc) error {
	spec, ok := SpecFor(engine)
	if !ok {
		return fmt.Errorf("unsupported engine %q", engine)
	}
	if exec == nil {
		return fmt.Errorf("exec function required")
	}
	if len(archive) == 0 {
		return fmt.Errorf("archive is empty")
	}
	if len(archive) > MaxArchiveBytes {
		return fmt.Errorf("archive is %d bytes; limit is %d", len(archive), MaxArchiveBytes)
	}

	cmd, err := restoreCommand(engine, creds)
	if err != nil {
		return err
	}

	_, stderr, err := exec(ctx, cmd, archive)
	if err != nil {
		return fmt.Errorf("%s failed: %w (%s)", spec.RestoreTool, err, strings.TrimSpace(string(stderr)))
	}
	return nil
}

func dumpCommand(engine Engine, creds Credentials) ([]string, string, error) {
	switch engine {
	case EngineMongoDB:
		cmd := []string{"mongodump", "--archive"}
		if creds.User != "" {
			cmd = append(cmd, "--username="+creds.User, "--password="+creds.Password, "--authenticationDatabase=admin")
		}
		return cmd, "application/octet-stream", nil

	case EnginePostgres:
		if creds.User == "" {
			return nil, "", fmt.Errorf("postgres dump requires a user")
		}
		// env prefix avoids an interactive password prompt inside the pod.
		cmd := []string{
			"env", "PGPASSWORD=" + creds.Password,
			"pg_dumpall",
			"-U", creds.User,
			"--clean",
			"--if-exists",
		}
		return cmd, "application/sql", nil

	case EngineMySQL:
		user := creds.User
		if user == "" {
			user = "root"
		}
		cmd := []string{
			"mysqldump",
			"-u", user,
			"--all-databases",
			"--single-transaction",
			"--routines",
			"--triggers",
		}
		if creds.Password != "" {
			cmd = append(cmd, "-p"+creds.Password)
		}
		return cmd, "application/sql", nil

	default:
		return nil, "", fmt.Errorf("unsupported engine %q", engine)
	}
}

func restoreCommand(engine Engine, creds Credentials) ([]string, error) {
	switch engine {
	case EngineMongoDB:
		cmd := []string{"mongorestore", "--archive", "--drop"}
		if creds.User != "" {
			cmd = append(cmd, "--username="+creds.User, "--password="+creds.Password, "--authenticationDatabase=admin")
		}
		return cmd, nil

	case EnginePostgres:
		if creds.User == "" {
			return nil, fmt.Errorf("postgres restore requires a user")
		}
		return []string{
			"env", "PGPASSWORD=" + creds.Password,
			"psql", "-U", creds.User, "-v", "ON_ERROR_STOP=1",
		}, nil

	case EngineMySQL:
		user := creds.User
		if user == "" {
			user = "root"
		}
		cmd := []string{"mysql", "-u", user}
		if creds.Password != "" {
			cmd = append(cmd, "-p"+creds.Password)
		}
		return cmd, nil

	default:
		return nil, fmt.Errorf("unsupported engine %q", engine)
	}
}
