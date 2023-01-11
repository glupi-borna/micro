package lsp

var servers_internal = []byte(`
- name: [ "rls", ]
  languages: [ "rust", ]
  command: [ "rls", ]
  install: [ [ "rustup", "update", ], [ "rustup", "component", "add", "rls", "rust-analysis", "rust-src", ], ]

- name: [ "typescript-language-server", ]
  languages: [ "javascript", ]
  command: [ "typescript-language-server", ]
  args: [ "--stdio", ]
  install: [ [ "npm", "install", "-g", "typescript-language-server", ], ]

- name: [ "typescript-language-server", ]
  languages: [ "typescript", ]
  command: [ "typescript-language-server", ]
  args: [ "--stdio", ]
  install: [ [ "npm", "install", "-g", "typescript-language-server", ], ]

- name: [ "html-languageserver", ]
  languages: [ "html", ]
  command: [ "html-languageserver", ]
  args: [ "--stdio", ]
  install: [ [ "npm", "install", "-g", "vscode-html-languageserver-bin", ], ]

- name: [ "ocaml-language-server", ]
  languages: [ "ocaml", ]
  command: [ "ocaml-language-server", ]
  args: [ "--stdio", ]
  install: [ [ "npm", "install", "-g", "ocaml-language-server", ], ]

- name: [ "pyls", ]
  languages: [ "python", ]
  command: [ "pyls", ]
  install: [ [ "pip", "install", "python-language-server", ], ]

- name: [ "clangd", ]
  languages: [ "c", ]
  command: [ "clangd", ]
  args: [ ]

- name: [ "clangd", ]
  languages: [ "cpp", ]
  command: [ "clangd", ]
  args: [ ]

- name: [ "hie", ]
  languages: [ "haskell", ]
  command: [ "hie", ]
  args: [ "--lsp", ]

- name: [ "gopls", ]
  languages: [ "go", ]
  command: [ "gopls", ]
  args: [ "serve", ]
  install: [ [ "go", "get", "-u", "golang.org/x/tools/gopls", ], ]

- name: [ "dart_language_server", ]
  languages: [ "dart", ]
  command: [ "dart_language_server", ]
  install: [ [ "pub", "global", "activate", "dart_language_server", ], ]

- name: [ "solargraph", ]
  languages: [ "ruby", ]
  command: [ "solargraph", ]
  args: [ "stdio", ]
  install: [ [ "gem", "install", "solargraph", ], ]

- name: [ "css-languageserver", ]
  languages: [ "css", ]
  command: [ "css-languageserver", ]
  args: [ "--stdio", ]
  install: [ [ "npm", "install", "-g", "vscode-css-languageserver-bin", ], ]

- name: [ "css-languageserver", ]
  languages: [ "scss", ]
  command: [ "css-languageserver", ]
  args: [ "--stdio", ]
  install: [ [ "npm", "install", "-g", "vscode-css-languageserver-bin", ], ]

- name: [ "vim-language-server", ]
  languages: [ "viml", ]
  command: [ "vim-language-server", ]
  args: [ "--stdio", ]
  install: [ [ "npm", "install", "-g", "vim-language-server", ], ]

- name: [ "purescript-language-server", ]
  languages: [ "purescript", ]
  command: [ "purescript-language-server", ]
  args: [ "--stdio", ]
  install: [ [ "npm", "install", "-g", "purescript-language-server", ], ]

- name: [ "svls", ]
  languages: [ "verilog", ]
  command: [ "svls", ]
  install: [ [ "cargo", "install", "svls", ], ]

- name: [ "serve-d", ]
  languages: [ "d", ]
  command: [ "serve-d", ]
`)
