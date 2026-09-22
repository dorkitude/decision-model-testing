package eval

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

var pairOptions = M{"Output (a)": "Output (a) is better.", "Output (b)": "Output (b) is better."}
var pairLabels = map[string]string{"Output (a)": "1", "Output (b)": "2"}

func label(s string) any {
	if s == "" {
		return nil
	}
	return s
}
func flip(v any) any {
	if v == "1" {
		return "2"
	}
	if v == "2" {
		return "1"
	}
	return v
}
func message(role, content string) M { return M{"role": role, "content": content} }
func levels(start int) []any {
	r := []any{}
	for i := start; i < start+10; i++ {
		r = append(r, strconv.Itoa(i))
	}
	return r
}

// Single-pass substitution: candidate braces are data, never template syntax.
func format(t string, values M, jinja bool) string {
	pattern := `\{([a-z_0-9]+)\}`
	if jinja {
		pattern = `\{\{\s*([a-z_0-9]+)\s*\}\}`
		t = strings.TrimSuffix(t, "\n")
	}
	re := regexp.MustCompile(pattern)
	return re.ReplaceAllStringFunc(t, func(s string) string {
		k := re.FindStringSubmatch(s)[1]
		v, ok := values[k]
		if !ok {
			panic(fmt.Errorf("missing template field %s", k))
		}
		return str(v)
	})
}
func chatML(prompt string) []any {
	var messages []any
	for _, part := range strings.Split(strings.TrimSpace(prompt), "<|im_start|>")[1:] {
		role, content, ok := strings.Cut(part, "\n")
		if !ok {
			panic("invalid chatML")
		}
		role = strings.TrimSpace(role)
		m := message(role, strings.TrimSpace(strings.SplitN(content, "<|im_end|>", 2)[0]))
		if strings.HasPrefix(role, "system ") {
			m["role"] = "system"
			for _, field := range strings.Fields(role)[1:] {
				k, v, _ := strings.Cut(field, "=")
				m[k] = v
			}
		}
		messages = append(messages, m)
	}
	return messages
}
func originalParse(text string, parsing M) any {
	for _, k := range Keys(parsing) {
		re := regexp.MustCompile(str(parsing[k]))
		if re.MatchString(strings.TrimSpace(text)) {
			return k
		}
	}
	return nil
}
func reverseNames(text string) string {
	return regexp.MustCompile(`(Output|output) \(([ab])\)`).ReplaceAllStringFunc(text, func(s string) string {
		if strings.HasSuffix(s, "(a)") {
			return strings.TrimSuffix(s, "(a)") + "(b)"
		}
		return strings.TrimSuffix(s, "(b)") + "(a)"
	})
}
func allValid(stages []any) bool {
	for _, s := range stages {
		if !yes(obj(s)["valid"]) {
			return false
		}
	}
	return true
}

func llmbar(root string, row M, method, model string, c *Client, key string) M {
	cfg := arr(ReadJSON(filepath.Join(root, "methodology/vendor/llmbar/configs", method+".json")))
	jev := strings.HasPrefix(model, "jev")
	stages := []any{}
	base := M{"input": row["input"]}
	call := func(module M, values M, stage, kind string) M {
		messages := chatML(format(string(Read(filepath.Join(root, "methodology/vendor/llmbar/prompts", str(module["prompt"])))), values, false))
		callKey := key + "/" + stage
		var r M
		if jev && kind != "auxiliary" {
			if kind == "rating" {
				r = c.Typed(callKey, model, messages, nil, levels(0))
				r["score"] = nil
				if yes(r["valid"]) {
					r["score"] = r["value"]
				}
			} else {
				r = c.Typed(callKey, model, messages, pairOptions, nil)
				r["decision"] = nil
				if yes(r["valid"]) {
					r["decision"] = label(pairLabels[str(r["value"])])
				}
				r["text"] = str(r["value"])
			}
		} else {
			selected := model
			if jev {
				selected = str(c.Config["helper_model"])
			}
			r = c.Chat(callKey, selected, messages, int(num(obj(module["openai_kwargs"])["max_tokens"])))
			if kind == "decision" {
				r["decision"] = originalParse(str(r["text"]), obj(module["parsing"]))
			}
			if kind == "rating" {
				r["score"] = nil
				if n, e := strconv.Atoi(strings.TrimSpace(str(r["text"]))); e == nil {
					r["score"] = float64(n)
				}
			}
		}
		stageRow := clone(r)
		stageRow["stage"] = stage
		stages = append(stages, stageRow)
		return r
	}
	swap := method == "Swap" || method == "Swap_CoT"
	rating := strings.HasPrefix(method, "Rating")
	if !swap {
		for i, v := range cfg[:len(cfg)-1] {
			r := call(obj(v), base, fmt.Sprintf("auxiliary-%d", i), "auxiliary")
			base[fmt.Sprintf("auxiliary_input_%d", i)] = strings.TrimSpace(str(r["text"]))
		}
	}
	module := obj(cfg[len(cfg)-1])
	if swap {
		module = obj(cfg[0])
	}
	if rating {
		scores := []any{}
		for i := 1; i <= 2; i++ {
			v := clone(base)
			v["output"] = row[fmt.Sprintf("output_%d", i)]
			scores = append(scores, call(module, v, fmt.Sprintf("rating-%d", i), "rating")["score"])
		}
		return M{"decisions": ratingPair(scores), "scores": scores, "valid": scores[0] != nil && scores[1] != nil && allValid(stages), "stages": stages}
	}
	initial := []M{}
	for i := 0; i < 2; i++ {
		v := clone(base)
		v["output_1"], v["output_2"] = row["output_1"], row["output_2"]
		if i == 1 {
			v["output_1"], v["output_2"] = v["output_2"], v["output_1"]
		}
		initial = append(initial, call(module, v, fmt.Sprintf("initial-%d", i), "decision"))
	}
	decisions := []any{initial[0]["decision"], flip(initial[1]["decision"])}
	if swap && ((decisions[0] == "1" && decisions[1] == "2") || (decisions[0] == "2" && decisions[1] == "1")) {
		winnerOrder := map[string]int{str(decisions[0]): 0, str(decisions[1]): 1}
		explanations := map[string]string{}
		for l, order := range winnerOrder {
			explanations[l] = str(initial[order]["text"])
		}
		final := []any{}
		for i := 0; i < 2; i++ {
			remapped := map[string]string{}
			for l, t := range explanations {
				if i != winnerOrder[l] {
					t = reverseNames(t)
				}
				remapped[l] = t
			}
			v := clone(base)
			a, b := "1", "2"
			if i == 1 {
				a, b = b, a
			}
			v["output_1"] = row["output_"+a]
			v["output_2"] = row["output_"+b]
			v["explanation_1"] = remapped[a]
			v["explanation_2"] = remapped[b]
			r := call(obj(cfg[1]), v, fmt.Sprintf("synthesis-%d", i), "decision")
			d := r["decision"]
			if i == 1 {
				d = flip(d)
			}
			final = append(final, d)
		}
		decisions = final
	}
	return M{"decisions": decisions, "valid": decisions[0] != nil && decisions[1] != nil && allValid(stages), "stages": stages}
}
func arenaParse(text string) (any, bool) {
	matches := regexp.MustCompile(`\[\[([AB<>=]+)\]\]`).FindAllStringSubmatch(text, -1)
	if len(matches) == 0 {
		return nil, true
	}
	first := matches[0][1]
	for _, m := range matches {
		if m[1] != first {
			return nil, false
		}
	}
	return label(map[string]string{"A>B": "1", "A>>B": "1", "B>A": "2", "B>>A": "2", "A=B": "TIE"}[first]), false
}
func judgebench(root string, row M, method, model string, c *Client, key string) M {
	stages := []any{}
	decisions := []any{}
	for i := 0; i < 2; i++ {
		a, b := row["output_1"], row["output_2"]
		if i == 1 {
			a, b = b, a
		}
		values := M{"question": row["input"], "prompt": row["input"], "answer_a": a, "answer_b": b}
		render := func(name string) string {
			return format(string(Read(filepath.Join(root, "methodology/.cache/judgebench/utils/templates", name+".jinja2"))), values, true)
		}
		var messages []any
		options := pairOptions
		if method == "vanilla" {
			messages = []any{message("user", render("vanilla_prompt"))}
		} else {
			messages = []any{message("system", render("arena_hard_judge_system")), message("user", render("arena_hard_judge_prompt"))}
			options = M{"A>>B": "A is significantly better.", "A>B": "A is slightly better.", "A=B": "Tie.", "B>A": "B is slightly better.", "B>>A": "B is significantly better."}
		}
		ck := fmt.Sprintf("%s/order-%d", key, i)
		var d any
		if strings.HasPrefix(model, "jev") {
			r := c.Typed(ck, model, messages, options, nil)
			if yes(r["valid"]) {
				if method == "vanilla" {
					d = label(pairLabels[str(r["value"])])
				} else {
					d = label(map[string]string{"A>>B": "1", "A>B": "1", "A=B": "TIE", "B>A": "2", "B>>A": "2"}[str(r["value"])])
				}
			}
			stages = append(stages, r)
		} else {
			attempts, tokens := 1, 1024
			if method == "arena_hard" {
				attempts, tokens = 2, 4096
			}
			judgment := ""
			for attempt := 0; attempt < attempts; attempt++ {
				r := c.Chat(fmt.Sprintf("%s/%d", ck, attempt), model, messages, tokens)
				stages = append(stages, r)
				judgment += "\n" + str(r["text"])
				if method == "vanilla" {
					d = label(pairLabels[strings.TrimSpace(str(r["text"]))])
					break
				}
				var again bool
				d, again = arenaParse(judgment)
				if !again {
					break
				}
				messages = append(messages, message("assistant", str(r["text"])), message("user", "continue your judgment and finish by outputting a final verdict label"))
			}
		}
		if i == 1 {
			d = flip(d)
		}
		decisions = append(decisions, d)
	}
	return M{"decisions": decisions, "stages": stages, "valid": decisions[0] != nil && decisions[1] != nil && allValid(stages)}
}
func rewardbench(root string, row M, method, model string, c *Client, key string, position int) M {
	rb := obj(ReadJSON(filepath.Join(root, "methodology/vendor/rewardbench/prompts.json")))
	stages := []any{}
	ties := row["subset"] == "Ties"
	if method == "fourway" && !ties {
		chosen, rejected := arr(row["chosen"]), arr(row["rejected"])
		if len(chosen) == 0 || len(rejected) < 3 {
			panic("four-way requires four responses")
		}
		answers := []any{chosen[0], rejected[0], rejected[1], rejected[2]}
		answers[0], answers[position] = answers[position], answers[0]
		v := M{"question": row["prompt"]}
		options := M{}
		for i, l := range []string{"A", "B", "C", "D"} {
			v["answer_"+strings.ToLower(l)] = answers[i]
			options[l] = "Assistant " + l + " is best."
		}
		messages := []any{message("system", str(rb["prompt_v2"])), message("user", format(str(rb["fourway_template"]), v, false))}
		var r M
		var choice any
		if strings.HasPrefix(model, "jev") {
			r = c.Typed(key+"/fourway", model, messages, options, nil)
			if yes(r["valid"]) {
				choice = r["value"]
			}
		} else {
			r = c.Chat(key+"/fourway", model, messages, 2048)
			for _, l := range []string{"A", "B", "C", "D"} {
				if strings.Contains(str(r["text"]), "[["+l+"]]") {
					choice = l
					break
				}
			}
		}
		score := .25
		if choice != nil {
			score = truth(choice == string("ABCD"[position]))
		}
		return M{"published_score": score, "choice": choice, "position": position, "valid": yes(r["valid"]) && choice != nil, "stages": []any{r}}
	}
	scores := []any{}
	answers := append(append([]any{}, arr(row["chosen"])...), arr(row["rejected"])...)
	allScores := true
	for i, answer := range answers {
		name := "ratings_prompt"
		if ties {
			name = "ratings_prompt_ties"
		}
		messages := []any{message("user", format(str(rb[name]), M{"prompt": row["prompt"], "completion": answer}, false))}
		ck := fmt.Sprintf("%s/rating-%d", key, i)
		var r M
		var score any
		if strings.HasPrefix(model, "jev") {
			r = c.Typed(ck, model, messages, nil, levels(1))
			if yes(r["valid"]) {
				score = num(r["value"]) + 1
			}
		} else {
			r = c.Chat(ck, model, messages, 1024)
			m := regexp.MustCompile(`\b([1-9]|10)\b\s*$`).FindStringSubmatch(strings.TrimSpace(str(r["text"])))
			if m != nil {
				n, e := strconv.Atoi(m[1])
				check(e)
				score = float64(n)
			}
		}
		if score == nil {
			allScores = false
		}
		stages = append(stages, r)
		scores = append(scores, score)
	}
	var published any
	if !ties {
		published = rewardRating(scores)
	}
	return M{"scores": scores, "num_correct": row["num_correct"], "valid": allScores && allValid(stages), "published_score": published, "stages": stages}
}
func Evaluate(root string, job M, c *Client) (result M, err error) {
	defer Recover(&err)
	b, m, model, key := str(job["benchmark"]), str(job["method"]), str(job["model"]), str(job["key"])
	row := obj(job["row"])
	switch b {
	case "llmbar":
		if nativeMethod(m) {
			result = nativePair(row, m, model, c, key)
			break
		}
		result = llmbar(root, row, m, model, c, key)
		result["metrics"] = llmbarPair(row["label"], arr(result["decisions"]))
		result["published_score"] = obj(result["metrics"])["correct_average"]
	case "judgebench":
		result = judgebench(root, row, m, model, c, key)
		result["published_score"] = judgePair(row["label"], arr(result["decisions"]))
		result["split"] = row["split"]
	case "rewardbench2":
		result = rewardbench(root, row, m, model, c, key, int(num(job["position"])))
	default:
		panic(fmt.Errorf("unknown benchmark %s", b))
	}
	result["key"], result["benchmark"], result["method"], result["model"], result["case_id"], result["subset"] = key, b, m, model, row["id"], row["subset"]
	return
}
