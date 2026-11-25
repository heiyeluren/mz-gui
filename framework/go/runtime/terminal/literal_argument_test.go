package terminal

import "testing"

func TestLiteralLastArgumentPreservesMultilineWithoutRelaxingOtherArguments(t *testing.T) {
	p := ProgramSpec{Executable: "/runtime/cli", Arguments: []string{"--", "/task 用户输入\n第二行\t原样保留"}, LiteralLastArgument: true}
	if !validProgram(p) {
		t.Fatal("直接传入程序的文本参数不应丢失换行")
	}
	p.LiteralLastArgument = false
	if validProgram(p) {
		t.Fatal("普通程序参数仍应使用默认约束")
	}
	p.LiteralLastArgument = true
	p.Arguments = []string{"bad\noption", "text"}
	if validProgram(p) {
		t.Fatal("文本标记放开了前面的程序选项")
	}
	for _, input := range []string{"text\x00suffix", "text\x1b[201~", "text\x03", "text\rsubmit"} {
		p.Arguments = []string{"--", input}
		if validProgram(p) {
			t.Fatal("允许了终端控制字符")
		}
	}
}
