package study

import "testing"

func TestUnseenQuestionsExcludeSharedTextDespiteDifferentIDs(t *testing.T) {
	unit := func(id, question string) Unit { return Unit{ID: id, Rows: []M{{"question_sha256": question}}} }
	dev := []paired{{A: unit("train-case", "shared")}}
	held := []paired{{A: unit("different-test-case", "shared")}, {A: unit("novel-case", "novel")}}
	got := unseenQuestionPairs(held, dev)
	if len(got) != 1 || got[0].A.ID != "novel-case" {
		t.Fatal(got)
	}
}
