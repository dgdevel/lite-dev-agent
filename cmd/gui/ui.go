package main

import (
	"fmt"
	"image"
	"image/color"
	"strings"

	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

type colorScheme struct {
	Background    color.NRGBA
	Surface       color.NRGBA
	Primary       color.NRGBA
	OnPrimary     color.NRGBA
	Text          color.NRGBA
	TextSecondary color.NRGBA
	Border        color.NRGBA
	Thinking      color.NRGBA
	Response      color.NRGBA
	ToolInput     color.NRGBA
	ToolOutput    color.NRGBA
	UserMsg       color.NRGBA
	TokenStats    color.NRGBA
	Error         color.NRGBA
	Stderr        color.NRGBA
	Success       color.NRGBA
	Denied        color.NRGBA
}

var colors = colorScheme{
	Background:    color.NRGBA{R: 30, G: 30, B: 30, A: 255},
	Surface:       color.NRGBA{R: 40, G: 40, B: 40, A: 255},
	Primary:       color.NRGBA{R: 98, G: 0, B: 238, A: 255},
	OnPrimary:     color.NRGBA{R: 255, G: 255, B: 255, A: 255},
	Text:          color.NRGBA{R: 220, G: 220, B: 220, A: 255},
	TextSecondary: color.NRGBA{R: 150, G: 150, B: 150, A: 255},
	Border:        color.NRGBA{R: 60, G: 60, B: 60, A: 255},
	Thinking:      color.NRGBA{R: 255, G: 138, B: 128, A: 255},
	Response:      color.NRGBA{R: 255, G: 255, B: 255, A: 255},
	ToolInput:     color.NRGBA{R: 105, G: 240, B: 174, A: 255},
	ToolOutput:    color.NRGBA{R: 105, G: 240, B: 174, A: 255},
	UserMsg:       color.NRGBA{R: 255, G: 213, B: 79, A: 255},
	TokenStats:    color.NRGBA{R: 128, G: 222, B: 234, A: 255},
	Error:         color.NRGBA{R: 255, G: 82, B: 82, A: 255},
	Stderr:        color.NRGBA{R: 255, G: 152, B: 0, A: 255},
	Success:       color.NRGBA{R: 76, G: 175, B: 80, A: 255},
	Denied:        color.NRGBA{R: 244, G: 67, B: 54, A: 255},
}

func blockColor(bt string) color.NRGBA {
	switch bt {
	case "agent_thinking":
		return colors.Thinking
	case "agent_response":
		return colors.Response
	case "tools_input":
		return colors.ToolInput
	case "tools_output":
		return colors.ToolOutput
	case "user_message":
		return colors.UserMsg
	case "token_stats":
		return colors.TokenStats
	case "stderr":
		return colors.Stderr
	default:
		return colors.TextSecondary
	}
}

// UI holds the widget state for the GUI.
type UI struct {
	theme       *material.Theme
	model       *AppModel
	bridge      *Bridge
	inputEditor widget.Editor
	sendBtn     widget.Clickable
	list        layout.List
	blockClicks []widget.Clickable
	scrollToEnd bool
	lastBlockCount int

	// Ask tool widgets
	askEditor     widget.Editor // for open-ended / alt cmd / reason
	askSendBtn    widget.Clickable
	askApproveBtn widget.Clickable
	askDenyBtn    widget.Clickable
	askOptionBtns []widget.Clickable
}

func NewUI(model *AppModel, bridge *Bridge) *UI {
	th := material.NewTheme()
	th.Palette = material.Palette{
		Fg:         colors.Text,
		Bg:         colors.Background,
		ContrastBg: colors.Primary,
		ContrastFg: colors.OnPrimary,
	}
	return &UI{
		theme:  th,
		model:  model,
		bridge: bridge,
		list:   layout.List{Axis: layout.Vertical},
		inputEditor: widget.Editor{
			SingleLine: false,
			Submit:     false,
		},
		askEditor: widget.Editor{
			SingleLine: false,
			Submit:     false,
		},
	}
}

func (ui *UI) Layout(gtx layout.Context) layout.Dimensions {
	// Handle keyboard shortcuts for main input
	for {
		ev, ok := gtx.Event(key.Filter{
			Focus:    &ui.inputEditor,
			Name:     key.NameReturn,
			Required: key.ModCtrl,
		})
		if !ok {
			break
		}
		if e, ok := ev.(key.Event); ok && e.State == key.Press {
			ui.sendPrompt()
		}
	}
	// Handle keyboard shortcuts for ask editor
	for {
		ev, ok := gtx.Event(key.Filter{
			Focus:    &ui.askEditor,
			Name:     key.NameReturn,
			Required: key.ModCtrl,
		})
		if !ok {
			break
		}
		if e, ok := ev.(key.Event); ok && e.State == key.Press {
			ui.sendAskResponse()
		}
	}
	if ui.sendBtn.Clicked(gtx) {
		ui.sendPrompt()
	}
	if ui.askSendBtn.Clicked(gtx) {
		ui.sendAskResponse()
	}
	if ui.askApproveBtn.Clicked(gtx) {
		ui.bridge.SendResponse("y")
	}
	if ui.askDenyBtn.Clicked(gtx) {
		ui.bridge.SendResponse("n")
	}

	paintRect(gtx, gtx.Constraints.Max, colors.Background)
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(ui.topBar),
		layout.Flexed(1, ui.blockList),
		layout.Rigid(ui.bottomArea),
	)
}

// bottomArea renders either the ask tool panel or the normal input area.
func (ui *UI) bottomArea(gtx layout.Context) layout.Dimensions {
	ui.model.mu.Lock()
	ask := ui.model.AskPending
	ui.model.mu.Unlock()

	if ask != nil {
		return ui.askPanel(gtx, ask)
	}
	return ui.inputArea(gtx)
}

// askPanel renders the appropriate UI for the pending ask tool interaction.
func (ui *UI) askPanel(gtx layout.Context, ask *AskState) layout.Dimensions {
	paintRect(gtx, image.Pt(gtx.Constraints.Max.X, 1), colors.Primary)

	return layout.UniformInset(unit.Dp(8)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return ui.askHeader(gtx, ask)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				switch ask.Type {
				case AskOpenEnded:
					return ui.askOpenEndedUI(gtx, ask)
				case AskMultipleChoice:
					return ui.askMultipleChoiceUI(gtx, ask)
				case AskExecConfirm:
					return ui.askExecConfirmUI(gtx, ask)
				case AskExecAltCmd:
					return ui.askExecAltCmdUI(gtx, ask)
				case AskExecReason:
					return ui.askExecReasonUI(gtx, ask)
				default:
					return layout.Dimensions{}
				}
			}),
		)
	})
}

func (ui *UI) askHeader(gtx layout.Context, ask *AskState) layout.Dimensions {
	var title string
	switch ask.Type {
	case AskOpenEnded:
		title = "💬 " + ask.Question
	case AskMultipleChoice:
		title = "📋 " + ask.Question
	case AskExecConfirm:
		title = "⚠️ Execute command?"
	case AskExecAltCmd:
		title = "🔧 Provide alternative command (or leave empty to give a reason):"
	case AskExecReason:
		title = "📝 Type the reason for denial:"
	}
	lbl := material.Body1(ui.theme, title)
	lbl.Color = colors.Primary
	lbl.TextSize = unit.Sp(14)
	return lbl.Layout(gtx)
}

func (ui *UI) askOpenEndedUI(gtx layout.Context, ask *AskState) layout.Dimensions {
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(layout.Spacer{Height: unit.Dp(4)}.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.askEditorRow(gtx, "Type your response... (Ctrl+Enter to send)")
		}),
	)
}

func (ui *UI) askMultipleChoiceUI(gtx layout.Context, ask *AskState) layout.Dimensions {
	// Ensure we have enough button widgets
	for len(ui.askOptionBtns) < len(ask.Options) {
		ui.askOptionBtns = append(ui.askOptionBtns, widget.Clickable{})
	}
	// Check for open-end text field
	if ask.AllowOpenEnd {
		for len(ui.askOptionBtns) < len(ask.Options)+1 {
			ui.askOptionBtns = append(ui.askOptionBtns, widget.Clickable{})
		}
	}

	children := []layout.FlexChild{
		layout.Rigid(layout.Spacer{Height: unit.Dp(4)}.Layout),
	}

	for i, opt := range ask.Options {
		idx := i
		opt := opt
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			click := &ui.askOptionBtns[idx]
			if click.Clicked(gtx) {
				ui.bridge.SendResponse(fmt.Sprintf("%d", idx+1))
			}
			btn := material.Button(ui.theme, click, opt)
			btn.Color = colors.Text
			btn.Background = colors.Surface
			btn.TextSize = unit.Sp(13)
			return btn.Layout(gtx)
		}))
		children = append(children, layout.Rigid(layout.Spacer{Height: unit.Dp(4)}.Layout))
	}

	if ask.AllowOpenEnd {
		openEndIdx := len(ask.Options)
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			click := &ui.askOptionBtns[openEndIdx]
			if click.Clicked(gtx) {
				// Send the open-end option number, then the user will type a response
				ui.bridge.SendResponse(fmt.Sprintf("%d", openEndIdx+1))
			}
			btn := material.Button(ui.theme, click, "✏️ Type your own response")
			btn.Color = colors.TextSecondary
			btn.Background = color.NRGBA{R: 50, G: 50, B: 50, A: 255}
			btn.TextSize = unit.Sp(13)
			return btn.Layout(gtx)
		}))
	}

	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
}

func (ui *UI) askExecConfirmUI(gtx layout.Context, ask *AskState) layout.Dimensions {
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(layout.Spacer{Height: unit.Dp(4)}.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			lbl := material.Body2(ui.theme, "  "+ask.Cmdline)
			lbl.Color = colors.ToolInput
			lbl.TextSize = unit.Sp(13)
			return lbl.Layout(gtx)
		}),
		layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					gtx.Constraints.Min.X = 100
					btn := material.Button(ui.theme, &ui.askApproveBtn, "✅ Approve")
					btn.Color = colors.OnPrimary
					btn.Background = colors.Success
					btn.TextSize = unit.Sp(13)
					return btn.Layout(gtx)
				}),
				layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					gtx.Constraints.Min.X = 100
					btn := material.Button(ui.theme, &ui.askDenyBtn, "❌ Deny")
					btn.Color = colors.OnPrimary
					btn.Background = colors.Denied
					btn.TextSize = unit.Sp(13)
					return btn.Layout(gtx)
				}),
			)
		}),
	)
}

func (ui *UI) askExecAltCmdUI(gtx layout.Context, ask *AskState) layout.Dimensions {
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(layout.Spacer{Height: unit.Dp(4)}.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.askEditorRow(gtx, "Type alternative command... (Ctrl+Enter to send)")
		}),
	)
}

func (ui *UI) askExecReasonUI(gtx layout.Context, ask *AskState) layout.Dimensions {
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(layout.Spacer{Height: unit.Dp(4)}.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.askEditorRow(gtx, "Type reason for denial... (Ctrl+Enter to send)")
		}),
	)
}

// askEditorRow renders the shared ask text editor with a send button.
func (ui *UI) askEditorRow(gtx layout.Context, hint string) layout.Dimensions {
	return layout.Flex{Axis: layout.Horizontal, Alignment: layout.End}.Layout(gtx,
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Min.Y = 60
			return layout.Stack{}.Layout(gtx,
				layout.Expanded(func(gtx layout.Context) layout.Dimensions {
					paintRect(gtx, image.Pt(gtx.Constraints.Max.X, gtx.Constraints.Min.Y),
						color.NRGBA{R: 50, G: 50, B: 50, A: 255})
					return layout.Dimensions{Size: image.Pt(gtx.Constraints.Max.X, gtx.Constraints.Min.Y)}
				}),
				layout.Stacked(func(gtx layout.Context) layout.Dimensions {
					return layout.UniformInset(unit.Dp(8)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						ed := material.Editor(ui.theme, &ui.askEditor, hint)
						ed.TextSize = unit.Sp(14)
						ed.Color = colors.Text
						ed.HintColor = colors.TextSecondary
						return ed.Layout(gtx)
					})
				}),
			)
		}),
		layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Min.Y = 40
			gtx.Constraints.Min.X = 80
			btn := material.Button(ui.theme, &ui.askSendBtn, "Send")
			btn.Color = colors.OnPrimary
			btn.Background = colors.Primary
			btn.TextSize = unit.Sp(14)
			return btn.Layout(gtx)
		}),
	)
}

func (ui *UI) sendAskResponse() {
	text := strings.TrimSpace(ui.askEditor.Text())
	ui.bridge.SendResponse(text)
	ui.askEditor.SetText("")
	ui.scrollToEnd = true
}

func (ui *UI) topBar(gtx layout.Context) layout.Dimensions {
	gtx.Constraints.Min.Y = 40
	return layout.Stack{}.Layout(gtx,
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			paintRect(gtx, image.Pt(gtx.Constraints.Max.X, gtx.Constraints.Min.Y), colors.Surface)
			paintRect(gtx, image.Pt(gtx.Constraints.Max.X, 1), colors.Border)
			return layout.Dimensions{Size: gtx.Constraints.Min}
		}),
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			return layout.UniformInset(unit.Dp(8)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						timeStr := ui.model.CurrentTime
						if timeStr == "" {
							timeStr = "--:--:--"
						}
						lbl := material.Body1(ui.theme, "🕐 "+timeStr)
						lbl.Color = colors.TextSecondary
						return lbl.Layout(gtx)
					}),
					layout.Rigid(layout.Spacer{Width: unit.Dp(24)}.Layout),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						prompt, completion := ui.model.TotalTokens()
						txt := fmt.Sprintf("🔤 Tokens — prompt: %d  completion: %d  total: %d", prompt, completion, prompt+completion)
						lbl := material.Body1(ui.theme, txt)
						lbl.Color = colors.TokenStats
						return lbl.Layout(gtx)
					}),
					layout.Rigid(layout.Spacer{Width: unit.Dp(24)}.Layout),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						ui.model.mu.Lock()
						started := ui.model.Started
						running := ui.model.Running
						ready := ui.model.Ready
						askPending := ui.model.AskPending
						ui.model.mu.Unlock()
						var txt string
						var clr color.NRGBA
						if askPending != nil {
							txt = "🔔 Awaiting Response"
							clr = colors.UserMsg
						} else if running {
							txt = "⏳ Processing..."
							clr = colors.Thinking
						} else if ready {
							txt = "✅ Ready"
							clr = colors.ToolInput
						} else if started {
							txt = "🟡 Starting..."
							clr = colors.UserMsg
						} else {
							txt = "🔴 Disconnected"
							clr = colors.Error
						}
						lbl := material.Body1(ui.theme, txt)
						lbl.Color = clr
						return lbl.Layout(gtx)
					}),
				)
			})
		}),
	)
}

func (ui *UI) blockList(gtx layout.Context) layout.Dimensions {
	ui.model.mu.Lock()
	blocks := make([]Block, len(ui.model.Blocks))
	copy(blocks, ui.model.Blocks)
	ui.model.mu.Unlock()

	var displayBlocks []Block
	var displayIndices []int
	for i, b := range blocks {
		if ShouldDisplay(b.BlockType) {
			displayBlocks = append(displayBlocks, b)
			displayIndices = append(displayIndices, i)
		}
	}
	for len(ui.blockClicks) < len(displayBlocks) {
	for len(ui.blockClicks) < len(displayBlocks) {
		ui.blockClicks = append(ui.blockClicks, widget.Clickable{})
	}

	// Auto-scroll when new blocks appear
	if len(displayBlocks) > ui.lastBlockCount {
		ui.scrollToEnd = true
		ui.lastBlockCount = len(displayBlocks)
	}

	if ui.scrollToEnd {
		ui.list.ScrollToEnd = true
		ui.scrollToEnd = false
	} else {
		ui.list.ScrollToEnd = false
	}

		ui.scrollToEnd = false
	}
	return ui.list.Layout(gtx, len(displayBlocks), func(gtx layout.Context, idx int) layout.Dimensions {
		b := displayBlocks[idx]
		click := &ui.blockClicks[idx]
		modelIdx := displayIndices[idx]

		if click.Clicked(gtx) {
			ui.model.ToggleBlock(modelIdx)
		}
		accent := blockColor(b.BlockType)
		label := BlockLabel(b.BlockType)

		return layout.UniformInset(unit.Dp(2)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return material.Clickable(gtx, click, func(gtx layout.Context) layout.Dimensions {
				return layout.Stack{}.Layout(gtx,
					layout.Expanded(func(gtx layout.Context) layout.Dimensions {
						paintRect(gtx, image.Pt(gtx.Constraints.Max.X, gtx.Constraints.Min.Y), colors.Surface)
						paintRect(gtx, image.Pt(3, gtx.Constraints.Min.Y), accent)
						return layout.Dimensions{Size: gtx.Constraints.Min}
					}),
					layout.Stacked(func(gtx layout.Context) layout.Dimensions {
						return layout.UniformInset(unit.Dp(8)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							headerText := ui.blockHeaderText(b, label, accent)
							bodyText := ui.blockBodyText(b)
							return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									lbl := material.Body2(ui.theme, headerText)
									lbl.Color = accent
									return lbl.Layout(gtx)
								}),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									if !b.Expanded || bodyText == "" {
										return layout.Dimensions{}
									}
									return layout.UniformInset(unit.Dp(4)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										lbl := material.Body2(ui.theme, bodyText)
										lbl.Color = colors.Text
										return lbl.Layout(gtx)
									})
								}),
							)
						})
					}),
				)
			})
		})
	})
}

func (ui *UI) blockHeaderText(b Block, label string, accent color.NRGBA) string {
	icon := "▸"
	if b.Expanded {
		icon = "▾"
	}
	switch b.BlockType {
	case "tools_definition":
		count := countTools(b.Content)
		return fmt.Sprintf("%s 📋 Tools (%d available)", icon, count)
	default:
		txt := fmt.Sprintf("%s %s", icon, label)
		if b.Duration != "" {
			txt += fmt.Sprintf("  (%s)", b.Duration)
		}
		if !b.Expanded && b.Content != "" {
			firstLine := b.Content
			if nl := strings.IndexByte(b.Content, '\n'); nl >= 0 {
				firstLine = b.Content[:nl]
			}
			if len(firstLine) > 80 {
				firstLine = firstLine[:80] + "…"
			}
			txt += ": " + firstLine
		}
		return txt
	}
}

func (ui *UI) blockBodyText(b Block) string {
	if b.BlockType == "tools_definition" {
		return formatToolDefs(b.Content)
	}
	return b.Content
}

func (ui *UI) inputArea(gtx layout.Context) layout.Dimensions {
	return layout.Stack{}.Layout(gtx,
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			paintRect(gtx, image.Pt(gtx.Constraints.Max.X, 1), colors.Border)
			return layout.Dimensions{Size: image.Pt(gtx.Constraints.Max.X, 1)}
		}),
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			return layout.UniformInset(unit.Dp(8)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal, Alignment: layout.End}.Layout(gtx,
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						gtx.Constraints.Min.Y = 80
						return layout.Stack{}.Layout(gtx,
							layout.Expanded(func(gtx layout.Context) layout.Dimensions {
								paintRect(gtx, image.Pt(gtx.Constraints.Max.X, gtx.Constraints.Min.Y),
									color.NRGBA{R: 50, G: 50, B: 50, A: 255})
								return layout.Dimensions{Size: image.Pt(gtx.Constraints.Max.X, gtx.Constraints.Min.Y)}
							}),
							layout.Stacked(func(gtx layout.Context) layout.Dimensions {
								return layout.UniformInset(unit.Dp(8)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
									ed := material.Editor(ui.theme, &ui.inputEditor, "Type your prompt... (Ctrl+Enter to send)")
									ed.TextSize = unit.Sp(14)
									ed.Color = colors.Text
									ed.HintColor = colors.TextSecondary
									return ed.Layout(gtx)
								})
							}),
						)
					}),
					layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						gtx.Constraints.Min.Y = 40
						gtx.Constraints.Min.X = 80
						btn := material.Button(ui.theme, &ui.sendBtn, "Send")
						btn.Color = colors.OnPrimary
						btn.Background = colors.Primary
						btn.TextSize = unit.Sp(14)
						return btn.Layout(gtx)
					}),
				)
			})
		}),
	)
}

func (ui *UI) sendPrompt() {
	text := strings.TrimSpace(ui.inputEditor.Text())
	if text == "" {
		return
	}
	ui.bridge.SendPrompt(text)
	ui.inputEditor.SetText("")
	ui.scrollToEnd = true
}

func paintRect(gtx layout.Context, size image.Point, c color.NRGBA) {
	defer clip.Rect{Max: size}.Push(gtx.Ops).Pop()
	paint.Fill(gtx.Ops, c)
}

// countTools counts tool entries in the raw tools_definition content.
// Each tool has a "Parameters:" line.
func countTools(content string) int {
	n := strings.Count(content, "\nParameters:")
	// Last tool may not have a trailing newline before Parameters
	if n == 0 && strings.Contains(content, "Parameters:") {
		return 1
	}
	return n
}

// formatToolDefs reformats raw tools_definition content into a readable display.
// Raw format: "tool_name: description\nParameters: {json}\ntool_name2: ..."
func formatToolDefs(raw string) string {
	lines := strings.Split(raw, "\n")
	type tool struct {
		name   string
		desc   string
		params string
	}
	var tools []tool
	var cur *tool

	for _, line := range lines {
		if strings.HasPrefix(line, "Parameters: ") {
			if cur != nil {
				cur.params = strings.TrimPrefix(line, "Parameters: ")
			}
			continue
		}
		// New tool: "name: description"
		if idx := strings.Index(line, ": "); idx > 0 {
			if cur != nil {
				tools = append(tools, *cur)
			}
			cur = &tool{
				name: line[:idx],
				desc: line[idx+2:],
			}
		}
	}
	if cur != nil {
		tools = append(tools, *cur)
	}

	var b strings.Builder
	for i, t := range tools {
		if i > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString("▸ ")
		b.WriteString(t.name)
		b.WriteString("\n  ")
		b.WriteString(t.desc)
		if t.params != "" {
			b.WriteString("\n  Parameters:\n")
			b.WriteString(indentString(prettyJSON(t.params), "    "))
		}
	}
	return b.String()
}

func indentString(s, prefix string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = prefix + l
	}
	return strings.Join(lines, "\n")
}

// prettyJSON does simple JSON indentation without importing encoding/json.
func prettyJSON(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "{") && !strings.HasPrefix(s, "[") {
		return s
	}
	var out strings.Builder
	indent := 0
	inStr := false
	esc := false
	for _, ch := range s {
		if esc {
			esc = false
			out.WriteRune(ch)
			continue
		}
		if ch == '\\' && inStr {
			esc = true
			out.WriteRune(ch)
			continue
		}
		if ch == '"' {
			inStr = !inStr
			out.WriteRune(ch)
			continue
		}
		if inStr {
			out.WriteRune(ch)
			continue
		}
		switch ch {
		case '{', '[':
			out.WriteRune(ch)
			indent++
			out.WriteRune('\n')
			out.WriteString(strings.Repeat("  ", indent))
		case '}', ']':
			indent--
			out.WriteRune('\n')
			out.WriteString(strings.Repeat("  ", indent))
			out.WriteRune(ch)
		case ',':
			out.WriteRune(ch)
			out.WriteRune('\n')
			out.WriteString(strings.Repeat("  ", indent))
		case ':':
			out.WriteString(": ")
		case ' ', '\t', '\n', '\r':
			// skip whitespace outside strings
		default:
			out.WriteRune(ch)
		}
	}
	return out.String()
}
