package translate

import (
	"bytes"
	"context"
	"fmt"
	"regexp"
	"strings"
	"text/template"

	log "github.com/sirupsen/logrus"
	"github.com/zjx20/hcfy-gemini/config"
	"github.com/zjx20/hcfy-gemini/gemini"
)

var (
	resultPattern = regexp.MustCompile(`(?ms)\s*(.*?)->(.*?)\n\s*(.*?)\z`)
	beginMarker   = "----begin----"
	endMarker     = "----end----"

	singleDestTemplate = template.Must(template.New("single_dest").Parse(`
你是一名翻译员，精通各国语言，尤其是英语和中文；同时你也精通各种计算机技术，习惯在 github 或 stackoverflow 等网站发表专业评论。
请帮我完成一些翻译，我现在会描述输入和输出的规则，真正需要翻译的内容我会在末尾给出。

输入要求：待翻译的内容被特殊标记包裹，每个段落以 "----begin----" 开始，以 "----end----" 结尾；可能存在多个段落。

输出要求：请按格式输出翻译结果，输出的第一行首先写从哪个语种翻译到哪个语种，格式为 "{source} -> {destination}"，语种用中文表达；紧接着输出每段的翻译，同样用 "----begin----" 和 "----end----" 包裹。

翻译要求：请把内容翻译成{{index .Dest 0}}，采用意译的翻译手法，含义准确，使用常见的单词和简练的句式，符合母语人士的表达习惯。必要时可以采用多阶段翻译，例如先直译一遍，然后在直译的基础上适当调整文法表达，或根据内容含义重新组织输出，最后再做一次精炼。每个段落独立翻译，每个段落都要有对应的翻译输出，即输入有多少段，输出就要有多少段。

另外请注意，有些段落可能整段都是一些无意义的 unicode 字符，这些内容可以直接输出，跳过翻译。再强调一遍，输出的段落数目要和输入一样，并且段落的顺序也要跟输入一致。

这里给出一个输入输出的示例：

	输入：
	----begin----
	hello
	----end----
	----begin----
	world
	----end----
	----begin----
	►
	----end----

	输出：
	英语 -> 中文
	----begin----
	你好
	----end----
	----begin----
	世界
	----end----
	----begin----
	►
	----end----

以下是待翻译内容：
{{- range $p := .Content  }}
{{ $p }}
{{- end }}
	`))

	multiDestTemplate = template.Must(template.New("multi_dest").Parse(`
你是一名翻译员，精通各国语言，尤其是英语和中文；同时你也精通各种计算机技术，习惯在 github 或 stackoverflow 等网站发表专业评论。
请帮我完成一些翻译，我现在会描述输入和输出的规则，真正需要翻译的内容我会在末尾给出。

输入要求：待翻译的内容被特殊标记包裹，每个段落以 "----begin----" 开始，以 "----end----" 结尾；可能存在多个段落。

输出要求：请按格式输出翻译结果，输出的第一行首先写从哪个语种翻译到哪个语种，格式为 "{source} -> {destination}"，语种用中文表达；紧接着输出每段的翻译，同样用 "----begin----" 和 "----end----" 包裹。

翻译要求：请把内容翻译成{{index .Dest 0}}。如果它已经是{{index .Dest 0}}，则把它翻译成{{index .Dest 1}}。采用意译的翻译手法，含义准确，使用常见的单词和简练的句式，符合母语人士的表达习惯。必要时可以采用多阶段翻译，例如先直译一遍，然后在直译的基础上适当调整文法表达，或根据内容含义重新组织输出，最后再做一次精炼。每个段落独立翻译，每个段落都要有对应的翻译输出，即输入有多少段，输出就要有多少段。

另外请注意，有些段落可能整段都是一些无意义的 unicode 字符，这些内容可以直接输出，跳过翻译。再强调一遍，输出的段落数目要和输入一样，并且段落的顺序也要跟输入一致。

这里给出一个输入输出的示例：

	输入：
	----begin----
	hello
	----end----
	----begin----
	world
	----end----
	----begin----
	►
	----end----

	输出：
	英语 -> 中文
	----begin----
	你好
	----end----
	----begin----
	世界
	----end----
	----begin----
	►
	----end----

以下是待翻译内容：
{{- range $p := .Content  }}
{{ $p }}
{{- end }}
	`))
)

type TranslateResult struct {
	Err  error
	Resp *TranslateResp
}

type session struct {
	dest   []string
	input  string
	respCh chan *TranslateResult
}

func newSession(dest []string, input string, respCh chan *TranslateResult) *session {
	return &session{
		dest:   dest,
		input:  input,
		respCh: respCh,
	}
}

func (s *session) fire(id string) {
	defer func() {
		if obj := recover(); obj != nil {
			err := fmt.Errorf("recovered from panic, err: %+v", obj)
			log.Errorf("%s", err)
			s.respCh <- &TranslateResult{Err: err}
		}
	}()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var tmpl *template.Template
	if len(s.dest) == 1 {
		tmpl = singleDestTemplate
	} else if len(s.dest) >= 2 {
		tmpl = multiDestTemplate
	}
	out := bytes.NewBuffer(nil)
	var content []string
	for _, p := range strings.Split(s.input, "\n") {
		content = append(content, beginMarker+"\n"+p+"\n"+endMarker)
	}
	tmpl.Execute(out, struct {
		Dest    []string
		Content []string
	}{
		Dest:    s.dest,
		Content: content,
	})

	ask := out.String()
	log.Debugf("ask: %s", ask)
	cfg := gemini.GenerateTextConfig{
		APIKey: config.ReadConfig().APIKey,
		Prompt: ask,
	}
	resp, err := gemini.GenerateText(ctx, cfg)
	if err != nil {
		log.Errorf("gemini err: %T \"%s\"", err, err.Error())
		s.respCh <- &TranslateResult{Err: err}
		return
	}
	log.Debugf("answer: %s", resp)

	translated := parseResp(resp)
	if translated == nil {
		log.Errorf("can't parse translate result from gemini, input: %q, response: %q",
			s.input, resp)
		err := fmt.Errorf("can't parse translate result from gemini")
		s.respCh <- &TranslateResult{Err: err}
		return
	}

	translated.Text = s.input
	s.respCh <- &TranslateResult{Resp: translated}
}

func parseResp(text string) *TranslateResp {
	var result *TranslateResp
	for _, matches := range resultPattern.FindAllStringSubmatch(text, -1) {
		content := matches[3]
		var paragraphs []string
		for {
			pos := strings.Index(content, beginMarker)
			if pos == -1 {
				break
			}
			content = content[pos+len(beginMarker):]
			pos = strings.Index(content, endMarker)
			if pos == -1 {
				break
			}
			paragraphs = append(paragraphs, strings.TrimSpace(content[:pos]))
			content = content[pos+len(endMarker):]
		}
		result = &TranslateResp{
			From:   strings.TrimSpace(matches[1]),
			To:     strings.TrimSpace(matches[2]),
			Result: paragraphs,
		}
	}
	return result
}
