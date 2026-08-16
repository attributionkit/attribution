package attribution

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const maxInfoPlistBytes = 1024 * 1024

func loadSwiftInfoPlist(project Project) (map[string]any, error) {
	if project.SwiftUI == nil {
		return nil, errors.New("not a SwiftUI project")
	}
	if project.SwiftUI.GeneratesInfoPlist || project.SwiftUI.InfoPlistPath == "" {
		return nil, errors.New("target uses generated Info.plist settings; configure one explicit project-relative INFOPLIST_FILE so nested attribution values are inspectable")
	}
	path := project.SwiftUI.InfoPlistPath
	if err := validateSafeTarget(project.Root, path); err != nil {
		return nil, err
	}
	absolute := filepath.Join(project.Root, filepath.FromSlash(path))
	info, err := os.Lstat(absolute)
	if err != nil {
		return nil, fmt.Errorf("inspect %s: %w", path, err)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() <= 0 || info.Size() > maxInfoPlistBytes {
		return nil, fmt.Errorf("%s must be a bounded regular non-symlink XML plist", path)
	}
	raw, err := os.ReadFile(absolute)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	value, err := decodeXMLPlist(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid %s: %w", path, err)
	}
	return value, nil
}

func decodeXMLPlist(raw []byte) (map[string]any, error) {
	if bytes.HasPrefix(raw, []byte("bplist")) {
		return nil, errors.New("binary plists are unsupported; convert this target Info.plist to XML")
	}
	decoder := xml.NewDecoder(bytes.NewReader(raw))
	var plistStart xml.StartElement
	for {
		token, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		if start, ok := token.(xml.StartElement); ok {
			if start.Name.Local != "plist" {
				return nil, errors.New("root element must be plist")
			}
			plistStart = start
			break
		}
	}
	_ = plistStart
	for {
		token, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		switch value := token.(type) {
		case xml.StartElement:
			parsed, err := parsePlistValue(decoder, value)
			if err != nil {
				return nil, err
			}
			object, ok := parsed.(map[string]any)
			if !ok {
				return nil, errors.New("plist root value must be a dictionary")
			}
			for {
				tail, tailErr := decoder.Token()
				if tailErr != nil {
					return nil, tailErr
				}
				switch end := tail.(type) {
				case xml.EndElement:
					if end.Name.Local == "plist" {
						for {
							extra, extraErr := decoder.Token()
							if extraErr == io.EOF {
								return object, nil
							}
							if extraErr != nil {
								return nil, extraErr
							}
							if whitespace, ok := extra.(xml.CharData); ok && strings.TrimSpace(string(whitespace)) == "" {
								continue
							}
							return nil, errors.New("unexpected content after plist")
						}
					}
				case xml.CharData:
					if strings.TrimSpace(string(end)) != "" {
						return nil, errors.New("unexpected text after plist value")
					}
				default:
					return nil, errors.New("unexpected content after plist value")
				}
			}
		case xml.CharData:
			if strings.TrimSpace(string(value)) != "" {
				return nil, errors.New("unexpected text before plist value")
			}
		case xml.EndElement:
			return nil, errors.New("plist has no value")
		}
	}
}

func parsePlistValue(decoder *xml.Decoder, start xml.StartElement) (any, error) {
	switch start.Name.Local {
	case "string", "key":
		var value string
		if err := decoder.DecodeElement(&value, &start); err != nil {
			return nil, err
		}
		return value, nil
	case "integer":
		var raw string
		if err := decoder.DecodeElement(&raw, &start); err != nil {
			return nil, err
		}
		value, err := strconv.Atoi(strings.TrimSpace(raw))
		if err != nil {
			return nil, errors.New("invalid plist integer")
		}
		return value, nil
	case "true":
		if err := decoder.Skip(); err != nil {
			return nil, err
		}
		return true, nil
	case "false":
		if err := decoder.Skip(); err != nil {
			return nil, err
		}
		return false, nil
	case "array":
		var values []any
		for {
			token, err := decoder.Token()
			if err != nil {
				return nil, err
			}
			switch value := token.(type) {
			case xml.StartElement:
				parsed, err := parsePlistValue(decoder, value)
				if err != nil {
					return nil, err
				}
				values = append(values, parsed)
			case xml.EndElement:
				if value.Name.Local == "array" {
					return values, nil
				}
			case xml.CharData:
				if strings.TrimSpace(string(value)) != "" {
					return nil, errors.New("unexpected text in plist array")
				}
			}
		}
	case "dict":
		object := map[string]any{}
		for {
			token, err := decoder.Token()
			if err != nil {
				return nil, err
			}
			switch value := token.(type) {
			case xml.EndElement:
				if value.Name.Local == "dict" {
					return object, nil
				}
			case xml.CharData:
				if strings.TrimSpace(string(value)) != "" {
					return nil, errors.New("unexpected text in plist dictionary")
				}
			case xml.StartElement:
				if value.Name.Local != "key" {
					return nil, errors.New("plist dictionary entry must start with key")
				}
				parsedKey, err := parsePlistValue(decoder, value)
				if err != nil {
					return nil, err
				}
				key := parsedKey.(string)
				if _, duplicate := object[key]; duplicate {
					return nil, fmt.Errorf("duplicate plist key %q", key)
				}
				for {
					next, nextErr := decoder.Token()
					if nextErr != nil {
						return nil, nextErr
					}
					if whitespace, ok := next.(xml.CharData); ok && strings.TrimSpace(string(whitespace)) == "" {
						continue
					}
					valueStart, ok := next.(xml.StartElement)
					if !ok {
						return nil, fmt.Errorf("plist key %q has no value", key)
					}
					parsedValue, err := parsePlistValue(decoder, valueStart)
					if err != nil {
						return nil, err
					}
					object[key] = parsedValue
					break
				}
			}
		}
	default:
		return nil, fmt.Errorf("unsupported plist value <%s>", start.Name.Local)
	}
}
