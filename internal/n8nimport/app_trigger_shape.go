package n8nimport

import "fmt"

// Payload shaping steps for webhook triggers whose n8n node rewrote the
// vendor's payload. They run as core.code after the trigger.webhook, whose
// item carries the request body under "body".

// shapeTypeform turns a Typeform form_response into answers keyed by
// question title (simplifyAnswers, default on) and, with onlyAnswers
// (default on), returns only those answers.
func shapeTypeform(c *MapContext) *ChainStep {
	simplify := c.Bool("simplifyAnswers", true)
	only := c.Bool("onlyAnswers", true)
	body := fmt.Sprintf(`var simplify = %s, onlyAnswers = %s;
function answerValue(a) {
  if (a.type === "choice") { return a.choice ? (a.choice.label !== undefined ? a.choice.label : a.choice.other) : null; }
  if (a.type === "choices") {
    var picked = (a.choices && a.choices.labels) || [];
    return a.choices && a.choices.other ? picked.concat([a.choices.other]) : picked;
  }
  return a[a.type];
}
return items.map(function (it) {
  var body = it.json.body !== undefined ? it.json.body : it.json;
  var resp = (body && body.form_response) || {};
  var answers = resp.answers || [];
  if (simplify) {
    var titles = {};
    ((resp.definition && resp.definition.fields) || []).forEach(function (f) { titles[f.id] = f.title; });
    var named = {};
    answers.forEach(function (a) {
      var f = a.field || {};
      named[titles[f.id] || f.ref || f.id] = answerValue(a);
    });
    answers = named;
  }
  if (onlyAnswers) { return simplify ? answers : { answers: answers }; }
  var out = Object.assign({}, body);
  out.form_response = Object.assign({}, resp, { answers: answers });
  return out;
});
`, jsValue(simplify), jsValue(only))
	return &ChainStep{Type: "core.code", Config: codeConfig(wrapCode(body))}
}

// shapeJotform decodes Jotform's rawRequest form field (a JSON string of
// q<N>_<name> answers) into answers keyed by the question's unique name;
// with onlyAnswers off the submission's ids come along.
func shapeJotform(c *MapContext) *ChainStep {
	only := c.Bool("onlyAnswers", true)
	if c.Bool("resolveData", true) {
		c.Note("answers are keyed by each question's unique name (n8n looked up the question labels through the Jotform API); app.jot_form form.getQuestions lists the labels")
	}
	body := fmt.Sprintf(`var onlyAnswers = %s;
return items.map(function (it) {
  var j = it.json.body !== undefined && it.json.body !== null && typeof it.json.body === "object" ? it.json.body : it.json;
  var raw = j.rawRequest, answers = {};
  if (typeof raw === "string") { try { raw = JSON.parse(raw); } catch (e) { raw = null; } }
  if (raw && typeof raw === "object") {
    Object.keys(raw).forEach(function (k) {
      var m = /^q\d+_(.+)$/.exec(k);
      if (m) { answers[m[1]] = raw[k]; }
    });
  }
  if (onlyAnswers) { return answers; }
  return { formID: j.formID, submissionID: j.submissionID, formTitle: j.formTitle, ip: j.ip, answers: answers, rawRequest: raw };
});
`, jsValue(only))
	return &ChainStep{Type: "core.code", Config: codeConfig(wrapCode(body))}
}
