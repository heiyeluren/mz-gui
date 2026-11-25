package updater

import (
	"context"
	"errors"
	"testing"
)

func TestRunInstallUpgradeMatrixCoversCleanOverlaySkipAndRollback(t *testing.T) {
	result, err := RunInstallUpgradeMatrix(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("本机安装升级矩阵应通过: %v", err)
	}
	if result.SchemaVersion != MatrixSchemaVersion || len(result.Cases) != 4 {
		t.Fatalf("矩阵结果错误: %#v", result)
	}
	want := []struct {
		name  string
		kind  string
		phase Phase
	}{
		{CaseCleanInstall, "full", PhaseCommitted},
		{CaseOverlayUpgrade, "delta", PhaseCommitted},
		{CaseSkipVersionUpgrade, "full", PhaseCommitted},
		{CaseRollbackOnValidate, "delta", PhaseRolledBack},
	}
	for index, caseResult := range result.Cases {
		if caseResult.Name != want[index].name || caseResult.SelectedKind != want[index].kind ||
			caseResult.Phase != want[index].phase || !caseResult.ReceiptOK || !caseResult.ProcessSetOK || caseResult.FallbackUsed {
			t.Fatalf("矩阵用例 %d 错误: %#v", index, caseResult)
		}
	}
}

func TestRunInstallUpgradeMatrixRejectsInvalidRoot(t *testing.T) {
	if _, err := RunInstallUpgradeMatrix(context.Background(), "relative"); !errors.Is(err, ErrInvalidMatrix) {
		t.Fatalf("相对路径必须拒绝: %v", err)
	}
	if _, err := RunInstallUpgradeMatrix(nil, t.TempDir()); !errors.Is(err, ErrInvalidMatrix) {
		t.Fatalf("空 context 必须拒绝: %v", err)
	}
}
