package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	sdkconnect "github.com/organic-programming/go-holons/pkg/connect"
	holonmetav1 "github.com/organic-programming/grace-op/gen/go/holonmeta/v1"
	"github.com/organic-programming/grace-op/internal/holons"
	inspectpkg "github.com/organic-programming/grace-op/pkg/inspect"
)

func cmdInspect(format Format, args []string) int {
	format, target, err := parseInspectArgs(format, args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "op inspect: %v\n", err)
		return 1
	}

	var doc *inspectpkg.Document
	if strings.Contains(target, ":") {
		doc, err = inspectRemote(target)
	} else {
		doc, err = inspectLocal(target)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "op inspect: %v\n", err)
		return 1
	}

	if format == FormatJSON {
		out, err := json.MarshalIndent(doc, "", "  ")
		if err != nil {
			fmt.Fprintf(os.Stderr, "op inspect: %v\n", err)
			return 1
		}
		fmt.Println(string(out))
		return 0
	}

	fmt.Print(inspectpkg.RenderText(doc))
	return 0
}

func parseInspectArgs(format Format, args []string) (Format, string, error) {
	currentFormat := format
	positional := make([]string, 0, len(args))

	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--json":
			currentFormat = FormatJSON
		case args[i] == "--format":
			if i+1 >= len(args) {
				return "", "", fmt.Errorf("--format requires a value")
			}
			parsed, err := parseFormat(args[i+1])
			if err != nil {
				return "", "", err
			}
			currentFormat = parsed
			i++
		case strings.HasPrefix(args[i], "--format="):
			parsed, err := parseFormat(strings.TrimPrefix(args[i], "--format="))
			if err != nil {
				return "", "", err
			}
			currentFormat = parsed
		default:
			positional = append(positional, args[i])
		}
	}

	if len(positional) != 1 {
		return "", "", fmt.Errorf("requires exactly one <slug> or <host:port>")
	}
	return currentFormat, positional[0], nil
}

func inspectLocal(ref string) (*inspectpkg.Document, error) {
	target, err := holons.ResolveTarget(ref)
	if err != nil {
		return nil, err
	}

	doc, err := inspectpkg.ParseProtoDir(filepath.Join(target.Dir, "protos"))
	if err != nil {
		return nil, err
	}

	doc.Slug = filepath.Base(target.Dir)
	if target.Identity != nil {
		if strings.TrimSpace(target.Identity.Motto) != "" {
			doc.Motto = strings.TrimSpace(target.Identity.Motto)
		}
	}
	if target.Manifest != nil {
		doc.Skills = manifestSkills(target.Manifest.Manifest.Skills)
	}

	return doc, nil
}

func inspectRemote(address string) (*inspectpkg.Document, error) {
	conn, err := sdkconnect.Connect(address)
	if err != nil {
		return nil, err
	}
	defer func() { _ = sdkconnect.Disconnect(conn) }()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client := holonmetav1.NewHolonMetaClient(conn)
	response, err := client.Describe(ctx, &holonmetav1.DescribeRequest{})
	if err != nil {
		return nil, err
	}

	return inspectpkg.FromDescribeResponse(response), nil
}

func manifestSkills(skills []holons.Skill) []inspectpkg.Skill {
	out := make([]inspectpkg.Skill, 0, len(skills))
	for _, skill := range skills {
		out = append(out, inspectpkg.Skill{
			Name:        strings.TrimSpace(skill.Name),
			Description: strings.TrimSpace(skill.Description),
			When:        strings.TrimSpace(skill.When),
			Steps:       append([]string(nil), skill.Steps...),
		})
	}
	return out
}
