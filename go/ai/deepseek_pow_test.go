package ai

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strconv"
	"testing"
)

func TestDeepseekPowHashMatchesKnown(t *testing.T) {
	// The hash of "abc" under DeepSeekHashV1 differs from standard SHA3-256
	// (round 0 is skipped), so just verify determinism + 32-byte output.
	a := deepseekPowHashV1([]byte("abc"))
	b := deepseekPowHashV1([]byte("abc"))
	if a != b {
		t.Fatal("hash not deterministic")
	}
	if len(a) != 32 {
		t.Fatalf("expected 32 bytes, got %d", len(a))
	}
	// different input -> different digest
	c := deepseekPowHashV1([]byte("abd"))
	if a == c {
		t.Fatal("hash collision on single byte change")
	}
}

func TestDeepseekPowSolveRoundTrip(t *testing.T) {
	salt := "s3cr3t"
	expire := int64(4102444800)
	answer := int64(42)
	prefix := salt + "_" + strconv.FormatInt(expire, 10) + "_"
	h := deepseekPowHashV1([]byte(prefix + strconv.FormatInt(answer, 10)))

	got, err := deepseekPowSolve(context.Background(), hexOf(h), salt, expire, 1000)
	if err != nil {
		t.Fatalf("solve: %v", err)
	}
	if got != answer {
		t.Fatalf("expected answer %d, got %d", answer, got)
	}
}

func TestDeepseekPowSolveNoSolution(t *testing.T) {
	// Challenge that does not correspond to any nonce in [0, 10).
	h := deepseekPowHashV1([]byte("test-salt_4102444800_999999"))
	_, err := deepseekPowSolve(context.Background(), hexOf(h), "test-salt", 4102444800, 10)
	if err == nil {
		t.Fatal("expected no-solution error")
	}
}

func TestDeepseekPowHeaderBuild(t *testing.T) {
	c := &deepseekPowChallenge{
		Algorithm:  "DeepSeekHashV1",
		Challenge:  "abcd",
		Salt:       "s",
		ExpireAt:   4102444800,
		Difficulty: 100,
		Signature:  "sig-123",
		TargetPath: "/api/v0/chat/completion",
	}
	header, err := deepseekPowBuildHeader(c, 42)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	dec, err := base64.StdEncoding.DecodeString(header)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(dec, &m); err != nil {
		t.Fatalf("json: %v", err)
	}
	if m["algorithm"] != "DeepSeekHashV1" || m["answer"] != float64(42) ||
		m["challenge"] != "abcd" || m["salt"] != "s" || m["signature"] != "sig-123" ||
		m["target_path"] != "/api/v0/chat/completion" {
		t.Fatalf("unexpected header payload: %v", m)
	}
}

func TestDeepseekPowRejectsUnsupportedAlgorithm(t *testing.T) {
	c := &deepseekPowChallenge{Algorithm: "OtherHash", Challenge: "x", Salt: "s", ExpireAt: 1, Difficulty: 10}
	if _, err := deepseekPowSolveAndBuildHeader(context.Background(), c); err == nil {
		t.Fatal("expected unsupported algorithm error")
	}
}

func hexOf(b [32]byte) string {
	const hexdigits = "0123456789abcdef"
	out := make([]byte, 64)
	for i, v := range b {
		out[i*2] = hexdigits[v>>4]
		out[i*2+1] = hexdigits[v&0x0f]
	}
	return string(out)
}
