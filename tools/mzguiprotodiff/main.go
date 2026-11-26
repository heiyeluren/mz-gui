package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

type protoToken struct {
	text   string
	quoted bool
}

type protoField struct {
	name    string
	number  int
	typeSig string
}

type protoMessage struct {
	fields         map[string]protoField
	fieldsByNumber map[int]protoField
	reservedNames  map[string]bool
	reservedRanges [][2]int
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("mzguiprotodiff", flag.ContinueOnError)
	flags.SetOutput(stderr)
	oldPath := flags.String("old", "", "旧 Protobuf 文件")
	newPath := flags.String("new", "", "新 Protobuf 文件")
	if err := flags.Parse(args); err != nil {
		return 1
	}
	if *oldPath == "" || *newPath == "" {
		fmt.Fprintln(stderr, "必须提供 --old 和 --new")
		return 1
	}
	oldData, err := readProtoPath(*oldPath)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	newData, err := readProtoPath(*newPath)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	issues := compareProto(string(oldData), string(newData))
	for _, issue := range issues {
		fmt.Fprintln(stdout, issue)
	}
	fmt.Fprintf(stdout, "Proto diff: breaking=%d\n", len(issues))
	if len(issues) > 0 {
		return 2
	}
	return 0
}

func readProtoPath(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return os.ReadFile(path)
	}
	paths := make([]string, 0)
	if err := filepath.WalkDir(path, func(candidate string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.IsDir() && filepath.Ext(candidate) == ".proto" {
			paths = append(paths, candidate)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	sort.Strings(paths)
	var combined strings.Builder
	for _, candidate := range paths {
		data, err := os.ReadFile(candidate)
		if err != nil {
			return nil, err
		}
		combined.Write(data)
		combined.WriteByte('\n')
	}
	return []byte(combined.String()), nil
}

func compareProto(oldSource, newSource string) []string {
	oldMessages := parseProto(oldSource)
	newMessages := parseProto(newSource)
	issues := make([]string, 0)
	for messageName, oldMessage := range oldMessages {
		newMessage, exists := newMessages[messageName]
		if !exists {
			issues = append(issues, "消息已删除: "+messageName)
			continue
		}
		for fieldName, oldField := range oldMessage.fields {
			newField, exists := newMessage.fields[fieldName]
			if exists {
				if oldField.number != newField.number {
					issues = append(issues, fmt.Sprintf("字段号改变: %s.%s %d -> %d", messageName, fieldName, oldField.number, newField.number))
				}
				if oldField.typeSig != newField.typeSig {
					issues = append(issues, fmt.Sprintf("字段类型改变: %s.%s %s -> %s", messageName, fieldName, oldField.typeSig, newField.typeSig))
				}
			} else if !oldMessageRemovalReserved(oldField, newMessage) {
				issues = append(issues, fmt.Sprintf("字段已删除且未 reserved: %s.%s", messageName, fieldName))
			}
			if replacement, reused := newMessage.fieldsByNumber[oldField.number]; reused && replacement.name != oldField.name {
				issues = append(issues, fmt.Sprintf("字段号被复用: %s.%s 的 %d 已分配给 %s", messageName, oldField.name, oldField.number, replacement.name))
			}
		}
		for _, newField := range newMessage.fields {
			if newMessage.reservedNames[newField.name] || numberReserved(newMessage.reservedRanges, newField.number) {
				issues = append(issues, fmt.Sprintf("reserved 名称或字段号被重新使用: %s.%s", messageName, newField.name))
			}
		}
	}
	sort.Strings(issues)
	return issues
}

func oldMessageRemovalReserved(field protoField, message protoMessage) bool {
	return message.reservedNames[field.name] || numberReserved(message.reservedRanges, field.number)
}

func numberReserved(ranges [][2]int, number int) bool {
	for _, item := range ranges {
		if number >= item[0] && number <= item[1] {
			return true
		}
	}
	return false
}

func parseProto(source string) map[string]protoMessage {
	tokens := tokenizeProto(source)
	messages := make(map[string]protoMessage)
	packageName := ""
	stack := make([]string, 0)
	for index := 0; index < len(tokens); {
		token := tokens[index].text
		if token == "package" && index+1 < len(tokens) {
			packageName = tokens[index+1].text
			index += 2
			continue
		}
		if token == "message" && index+2 < len(tokens) {
			name := tokens[index+1].text
			fullName := joinProtoName(packageName, append(stack, name))
			stack = append(stack, name)
			messages[fullName] = protoMessage{
				fields: make(map[string]protoField), fieldsByNumber: make(map[int]protoField), reservedNames: make(map[string]bool),
			}
			index = advanceToToken(tokens, index+2, "{") + 1
			continue
		}
		if token == "}" && len(stack) > 0 {
			stack = stack[:len(stack)-1]
			index++
			continue
		}
		if token == "enum" || token == "service" {
			index = skipProtoBlock(tokens, index)
			continue
		}
		if len(stack) > 0 && token == "reserved" {
			messageName := joinProtoName(packageName, stack)
			message := messages[messageName]
			end := advanceToToken(tokens, index, ";")
			parseReserved(tokens[index+1:end], &message)
			messages[messageName] = message
			index = end + 1
			continue
		}
		if len(stack) > 0 {
			messageName := joinProtoName(packageName, stack)
			if field, next, ok := parseProtoField(tokens, index); ok {
				message := messages[messageName]
				message.fields[field.name] = field
				message.fieldsByNumber[field.number] = field
				messages[messageName] = message
				index = next
				continue
			}
		}
		index++
	}
	return messages
}

func parseProtoField(tokens []protoToken, start int) (protoField, int, bool) {
	index := start
	label := ""
	if tokens[index].text == "optional" || tokens[index].text == "required" || tokens[index].text == "repeated" {
		label = tokens[index].text + " "
		index++
	}
	if index >= len(tokens) || !isProtoIdentifier(tokens[index].text) {
		return protoField{}, start, false
	}
	typeSig := label + tokens[index].text
	index++
	if typeSig == label+"map" && index < len(tokens) && tokens[index].text == "<" {
		for index < len(tokens) {
			typeSig += tokens[index].text
			index++
			if tokens[index-1].text == ">" {
				break
			}
		}
	}
	if index+3 >= len(tokens) || !isProtoIdentifier(tokens[index].text) || tokens[index+1].text != "=" {
		return protoField{}, start, false
	}
	number, err := strconv.Atoi(tokens[index+2].text)
	if err != nil || number <= 0 {
		return protoField{}, start, false
	}
	name := tokens[index].text
	index = advanceToToken(tokens, index+3, ";")
	return protoField{name: name, number: number, typeSig: typeSig}, index + 1, true
}

func parseReserved(tokens []protoToken, message *protoMessage) {
	for index := 0; index < len(tokens); {
		if tokens[index].quoted {
			message.reservedNames[tokens[index].text] = true
			index++
			continue
		}
		start, err := strconv.Atoi(tokens[index].text)
		if err == nil && start > 0 {
			end := start
			if index+2 < len(tokens) && tokens[index+1].text == "to" {
				if value, parseErr := strconv.Atoi(tokens[index+2].text); parseErr == nil {
					end = value
					index += 2
				}
			}
			message.reservedRanges = append(message.reservedRanges, [2]int{start, end})
		}
		index++
	}
}

func tokenizeProto(source string) []protoToken {
	tokens := make([]protoToken, 0)
	runes := []rune(source)
	for index := 0; index < len(runes); {
		if unicode.IsSpace(runes[index]) {
			index++
			continue
		}
		if runes[index] == '/' && index+1 < len(runes) && runes[index+1] == '/' {
			index += 2
			for index < len(runes) && runes[index] != '\n' {
				index++
			}
			continue
		}
		if runes[index] == '/' && index+1 < len(runes) && runes[index+1] == '*' {
			index += 2
			for index+1 < len(runes) && !(runes[index] == '*' && runes[index+1] == '/') {
				index++
			}
			index += 2
			continue
		}
		if runes[index] == '"' || runes[index] == '\'' {
			quote := runes[index]
			index++
			value := make([]rune, 0)
			for index < len(runes) && runes[index] != quote {
				if runes[index] == '\\' && index+1 < len(runes) {
					index++
				}
				value = append(value, runes[index])
				index++
			}
			index++
			tokens = append(tokens, protoToken{text: string(value), quoted: true})
			continue
		}
		if unicode.IsLetter(runes[index]) || runes[index] == '_' || runes[index] == '.' {
			start := index
			index++
			for index < len(runes) && (unicode.IsLetter(runes[index]) || unicode.IsDigit(runes[index]) || runes[index] == '_' || runes[index] == '.') {
				index++
			}
			tokens = append(tokens, protoToken{text: string(runes[start:index])})
			continue
		}
		if unicode.IsDigit(runes[index]) {
			start := index
			index++
			for index < len(runes) && unicode.IsDigit(runes[index]) {
				index++
			}
			tokens = append(tokens, protoToken{text: string(runes[start:index])})
			continue
		}
		tokens = append(tokens, protoToken{text: string(runes[index])})
		index++
	}
	return tokens
}

func advanceToToken(tokens []protoToken, start int, value string) int {
	for index := start; index < len(tokens); index++ {
		if tokens[index].text == value {
			return index
		}
	}
	return len(tokens)
}

func skipProtoBlock(tokens []protoToken, start int) int {
	index := advanceToToken(tokens, start, "{")
	depth := 0
	for index < len(tokens) {
		switch tokens[index].text {
		case "{":
			depth++
		case "}":
			depth--
			if depth == 0 {
				return index + 1
			}
		}
		index++
	}
	return len(tokens)
}

func joinProtoName(packageName string, names []string) string {
	parts := append([]string{}, names...)
	if packageName != "" {
		parts = append([]string{packageName}, parts...)
	}
	return strings.Join(parts, ".")
}

func isProtoIdentifier(value string) bool {
	if value == "" {
		return false
	}
	first := []rune(value)[0]
	return unicode.IsLetter(first) || first == '_' || first == '.'
}
