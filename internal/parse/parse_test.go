// Tests for the source parsers (Go, Python, TypeScript), DetectLanguage, and
// FormatContext.

package parse

import (
	"os"
	"strings"
	"testing"

	"github.com/qbtrix/kb-go/internal/kbtest"
)

func TestParseGoBasic(t *testing.T) {
	source := `package main

import (
	"fmt"
	"net/http"
)

// Server handles HTTP requests.
type Server struct {
	Port int
	Host string
}

// Start boots the server.
func (s *Server) Start() error {
	return nil
}

func (s *Server) Stop() {}

// HealthCheck is a standalone function.
func HealthCheck(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintln(w, "ok")
}

type Handler interface {
	ServeHTTP(w http.ResponseWriter, r *http.Request)
}

const MaxRetries = 3
`
	mod := parseGo("main.go", source)
	if mod == nil {
		t.Fatal("parseGo returned nil")
	}
	if mod.Language != "go" {
		t.Errorf("Language = %q", mod.Language)
	}
	if mod.Package != "main" {
		t.Errorf("Package = %q", mod.Package)
	}
	if len(mod.Imports) != 2 {
		t.Errorf("Imports = %v", mod.Imports)
	}

	// Should have Server struct, Handler interface
	if len(mod.Types) < 2 {
		t.Fatalf("Types count = %d, want >= 2", len(mod.Types))
	}

	// Find Server struct
	var server *CodeType
	for i, typ := range mod.Types {
		if typ.Name == "Server" {
			server = &mod.Types[i]
			break
		}
	}
	if server == nil {
		t.Fatal("Server struct not found")
	}
	if server.Kind != "struct" {
		t.Errorf("Server.Kind = %q", server.Kind)
	}
	if len(server.Fields) != 2 {
		t.Errorf("Server.Fields = %v", server.Fields)
	}
	if len(server.Methods) != 2 {
		t.Errorf("Server should have 2 methods (Start, Stop), got %d", len(server.Methods))
	}

	// Find Handler interface
	var handler *CodeType
	for i, typ := range mod.Types {
		if typ.Name == "Handler" {
			handler = &mod.Types[i]
		}
	}
	if handler == nil {
		t.Fatal("Handler interface not found")
	}
	if handler.Kind != "interface" {
		t.Errorf("Handler.Kind = %q", handler.Kind)
	}

	// HealthCheck should be a top-level function
	if len(mod.Functions) < 1 {
		t.Fatal("expected at least 1 top-level function")
	}
	if mod.Functions[0].Name != "HealthCheck" {
		t.Errorf("Function name = %q", mod.Functions[0].Name)
	}
}

func TestParseGoInvalidSyntax(t *testing.T) {
	mod := parseGo("bad.go", "this is not go code {{{")
	if mod != nil {
		t.Error("expected nil for invalid Go")
	}
}

func TestParsePythonBasic(t *testing.T) {
	source := `"""Module docstring."""

import os
from pathlib import Path
from typing import Optional

MAX_RETRIES = 3
DEFAULT_TIMEOUT = 30

class UserService(BaseService):
    """Handles user operations."""

    async def create_user(self, name: str, email: str) -> dict:
        pass

    def get_user(self, user_id: int) -> Optional[dict]:
        pass

class AdminService(UserService):
    pass

def health_check(request) -> str:
    """Check system health."""
    return "ok"

async def process_batch(items: list, limit: int = 10):
    pass

def _private_helper():
    pass
`
	mod := parsePython("service.py", source)
	if mod == nil {
		t.Fatal("parsePython returned nil")
	}
	if mod.Language != "python" {
		t.Errorf("Language = %q", mod.Language)
	}
	if mod.Docstring != "Module docstring." {
		t.Errorf("Docstring = %q", mod.Docstring)
	}

	// Imports
	if len(mod.Imports) < 3 {
		t.Errorf("Imports = %v", mod.Imports)
	}

	// Constants
	if len(mod.Constants) < 2 {
		t.Errorf("Constants = %v, want at least MAX_RETRIES and DEFAULT_TIMEOUT", mod.Constants)
	}

	// Classes
	if len(mod.Types) != 2 {
		t.Fatalf("Types count = %d, want 2", len(mod.Types))
	}
	if mod.Types[0].Name != "UserService" {
		t.Errorf("Types[0].Name = %q", mod.Types[0].Name)
	}
	if mod.Types[0].Kind != "class" {
		t.Errorf("Types[0].Kind = %q", mod.Types[0].Kind)
	}
	if len(mod.Types[0].Bases) == 0 || mod.Types[0].Bases[0] != "BaseService" {
		t.Errorf("Types[0].Bases = %v", mod.Types[0].Bases)
	}
	if mod.Types[0].Docstring != "Handles user operations." {
		t.Errorf("Types[0].Docstring = %q", mod.Types[0].Docstring)
	}

	// Methods
	if len(mod.Types[0].Methods) != 2 {
		t.Errorf("UserService methods = %d, want 2", len(mod.Types[0].Methods))
	}
	if len(mod.Types[0].Methods) > 0 && !mod.Types[0].Methods[0].IsAsync {
		t.Error("create_user should be async")
	}

	// Top-level functions
	if len(mod.Functions) < 2 {
		t.Fatalf("Functions = %d, want >= 2", len(mod.Functions))
	}
}

func TestParseTypeScriptBasic(t *testing.T) {
	source := `import { Request, Response } from 'express'
import { UserModel } from './models/user'
import jwt from 'jsonwebtoken'

export interface AuthProvider {
	authenticate(token: string): Promise<User>
}

export class AuthService extends BaseService implements AuthProvider {
	async authenticate(token: string): Promise<User> {
		return jwt.verify(token)
	}
}

export type UserId = string

export enum Role {
	Admin,
	User,
	Guest,
}

export async function createUser(name: string, email: string): Promise<User> {
	return new User(name, email)
}

export const handleRequest = async (req: Request, res: Response) => {
	res.json({ ok: true })
}

function internalHelper(x: number): boolean {
	return x > 0
}
`
	mod := parseTypeScript("auth.ts", source, "typescript")
	if mod == nil {
		t.Fatal("parseTypeScript returned nil")
	}
	if mod.Language != "typescript" {
		t.Errorf("Language = %q", mod.Language)
	}

	// Imports
	if len(mod.Imports) != 3 {
		t.Errorf("Imports = %v, want 3", mod.Imports)
	}

	// Types: AuthProvider (interface), AuthService (class), UserId (type), Role (enum)
	if len(mod.Types) != 4 {
		t.Fatalf("Types = %d, want 4: %+v", len(mod.Types), mod.Types)
	}

	// Check interface
	found := false
	for _, typ := range mod.Types {
		if typ.Name == "AuthProvider" && typ.Kind == "interface" {
			found = true
		}
	}
	if !found {
		t.Error("AuthProvider interface not found")
	}

	// Check class with extends + implements
	for _, typ := range mod.Types {
		if typ.Name == "AuthService" {
			if typ.Kind != "class" {
				t.Errorf("AuthService.Kind = %q", typ.Kind)
			}
			if len(typ.Bases) < 2 {
				t.Errorf("AuthService.Bases = %v, want BaseService + AuthProvider", typ.Bases)
			}
			if !typ.IsExported {
				t.Error("AuthService should be exported")
			}
		}
	}

	// Functions: createUser, handleRequest (arrow), internalHelper
	if len(mod.Functions) < 2 {
		t.Errorf("Functions = %d, want >= 2", len(mod.Functions))
	}
}

func TestFormatCodeContext(t *testing.T) {
	mod := &Module{
		Language: "go",
		Package:  "main",
		Imports:  []string{"fmt", "net/http"},
		Types: []CodeType{
			{Name: "Server", Kind: "struct", Fields: []string{"Port", "Host"}, IsExported: true},
		},
		Functions: []CodeFunc{
			{Name: "main", Args: nil, IsExported: false},
		},
	}

	output := FormatContext(mod)
	if !strings.Contains(output, "Language: go") {
		t.Error("missing language")
	}
	if !strings.Contains(output, "Package: main") {
		t.Error("missing package")
	}
	if !strings.Contains(output, "struct Server") {
		t.Error("missing struct Server")
	}
	if !strings.Contains(output, "main()") {
		t.Error("missing main function")
	}
}

func TestDetectLanguage(t *testing.T) {
	tests := map[string]string{
		"main.go":       "go",
		"service.py":    "python",
		"app.ts":        "typescript",
		"component.tsx": "typescript",
		"index.js":      "javascript",
		"readme.md":     "",
		"data.json":     "",
	}
	for path, want := range tests {
		got := DetectLanguage(path)
		if got != want {
			t.Errorf("detectLanguage(%q) = %q, want %q", path, got, want)
		}
	}
}

func TestMain(m *testing.M) {
	os.Exit(kbtest.Main(m))
}
