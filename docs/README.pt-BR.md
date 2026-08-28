# sam-harness

[English](../README.md) | [Español](README.es.md)

O Sam Harness transforma a arquitetura, os comandos, o caminho até produção e os riscos reais de um repositório em instruções duráveis para agentes de programação. Ele não joga um prompt enorme em toda conversa. Uma skill portátil orienta a adoção, um CLI em Go cria planos e checks determinísticos, e os arquivos instalados mantêm as regras ativas depois que o chat termina.

O método vem do livro [Development Harness](https://www.samuelfaj.com/books/development-harness-course/development-harness-course.html), de Samuel Fajreldines.

[![Fluxo do Development Harness, das regras do repositório à produção segura](../assets/sam-harness.png)](https://www.samuelfaj.com/books/development-harness-course/development-harness-course.html)

Leia o livro: [🇧🇷 Português](https://www.samuelfaj.com/books/development-harness-course/development-harness-course.html) · [🇺🇸 English](https://www.samuelfaj.com/books/development-harness-course/development-harness-course.en-US.html) · [🇪🇸 Español](https://www.samuelfaj.com/books/development-harness-course/development-harness-course.es.html)

## Instale a skill

```bash
npx skills add samuelfaj/sam-harness --skill sam-harness -g
```

Entre em um repositório e diga ao agente:

```text
Aplique o sam-harness aqui.
```

Quando o agente aceitar invocação explícita, prefira:

```text
Use $sam-harness e aplique o harness neste repositório.
```

```text
/sam-harness apply
```

A skill pede autorização antes de baixar o CLI. Os scripts de bootstrap exigem Cosign e conferem o certificado da release e o checksum antes de instalar o binário no cache do usuário.

## Como funciona

1. `scan` lê manifests, comandos, workspaces, CI, estado do Git e indícios de interface, persistência e deploy. Ele não edita o repositório.
2. O agente pergunta apenas sobre fatos de negócio que o código não prova, como criticidade, sensibilidade dos dados, uso em produção, autoridade, design, rollback, responsáveis, comandos ambíguos e um provedor de CI não detectado.
3. `plan` recomenda `baseline`, `production` ou `regulated` e lista cada operação sob um identificador criptográfico que expira após 30 minutos.
4. O usuário revisa e aprova esse identificador.
5. `apply` recusa um plano se o repositório mudou e grava somente as operações aprovadas.
6. `doctor` verifica a estrutura instalada. `check` executa os comandos configurados e grava um recibo local.

O Sam Harness preserva o conteúdo existente de `AGENTS.md`, `CLAUDE.md`, `GEMINI.md`, instruções do Copilot, `.gitignore` e GitLab CI por meio de blocos delimitados. Ele não concede autoridade para commit, push, release ou deploy.

## Comandos

```text
sam-harness scan [path] [--format human|json]
sam-harness plan [path] --profile auto|baseline|production|regulated [--answers arquivo]
sam-harness apply --plan <arquivo> --accept <plan-id>
sam-harness check [path] [--format human|json]
sam-harness doctor [path]
sam-harness upgrade [path] --to <versão>
```

Por padrão, os planos ficam no diretório temporário do sistema operacional. `--output` só aceita um arquivo novo fora do repositório. Arquivos existentes e caminhos internos são recusados. `scan` e `plan` não alteram arquivos versionados.

## Perfis

`baseline` instala o contrato do repositório, limites de autoridade, gates locais, regras de evidência e controles de qualidade para interfaces.

`production` acrescenta CI, runbooks de release e rollback, artefato imutável, SBOM, proveniência, reconciliação de migrações e observação em produção.

Esses controles declaram requisitos, não provas. A promoção ainda exige comprovantes da execução real da CI, digest do artefato, SBOM, proveniência, aprovações e observação do sistema em produção.

`regulated` acrescenta threat model, governança de dados, separação de aprovações, evidência de auditoria, exercícios de recuperação e aposentadoria. O perfil não certifica conformidade regulatória.

## Stacks suportadas

A primeira versão detecta TypeScript e JavaScript, Python, Go e Rust, inclusive em monorepos mistos. A integração com GitHub Actions e GitLab CI só entra no plano quando o usuário autoriza mudanças de CI.

## Desenvolvimento

O projeto usa Go 1.27.

```bash
go test ./...
go test -race ./...
go vet ./...
go build ./cmd/sam-harness
python3 scripts/validate-skill.py skills/sam-harness
```

A [matriz de rastreabilidade](book-traceability.md) liga os 20 capítulos do livro a controles, perguntas, templates ou testes. O CI falha se algum capítulo ficar sem representação.

## Segurança e licença

Consulte [SECURITY.md](../SECURITY.md) para relatar vulnerabilidades. As releases incluem checksums, assinatura keyless do Cosign, SBOMs CycloneDX e proveniência de build do GitHub.

Licença MIT. Copyright 2026 Samuel Fajreldines.
