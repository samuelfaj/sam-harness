# sam-harness

[English](../README.md) | [Português](README.pt-BR.md)

Sam Harness convierte la arquitectura, los comandos, el recorrido hasta producción y los riesgos reales de un repositorio en instrucciones duraderas para agentes de programación. No inserta un prompt enorme en cada conversación. Una skill portátil guía la adopción, un CLI escrito en Go crea planes y controles deterministas, y los archivos instalados mantienen las reglas después de cerrar el chat.

El método procede del libro [Development Harness](https://www.samuelfaj.com/books/development-harness-couse/development-harness-course.es.html), de Samuel Fajreldines.

[![Flujo de Development Harness, desde las reglas del repositorio hasta producción segura](../assets/sam-harness.png)](https://www.samuelfaj.com/books/development-harness-couse/development-harness-course.es.html)

Lee el libro: [🇪🇸 Español](https://www.samuelfaj.com/books/development-harness-couse/development-harness-course.es.html) · [🇺🇸 English](https://www.samuelfaj.com/books/development-harness-couse/development-harness-course.en-US.html) · [🇧🇷 Português](https://www.samuelfaj.com/books/development-harness-couse/development-harness-course.html)

## Instala la skill

```bash
npx skills add samuelfaj/sam-harness --skill sam-harness -g
```

Entra en un repositorio y dile al agente:

```text
Aplica sam-harness aquí.
```

Si el agente admite una invocación explícita, usa:

```text
Use $sam-harness and apply the harness to this repository.
```

```text
/sam-harness apply
```

La skill pide permiso antes de descargar el CLI. Los scripts de instalación exigen Cosign y comprueban el certificado de la release y el checksum antes de instalar el binario en la caché del usuario.

## Cómo funciona

1. `scan` lee manifiestos, comandos, workspaces, CI, estado de Git e indicios de interfaz, persistencia y despliegue. No modifica el repositorio.
2. El agente pregunta por los hechos de negocio que el código no puede demostrar, como criticidad, sensibilidad de datos, uso en producción, autoridad, diseño, rollback, responsables, comandos ambiguos y un proveedor de CI no detectado.
3. `plan` recomienda `baseline`, `production` o `regulated` y enumera las operaciones bajo un identificador criptográfico que caduca después de 30 minutos.
4. El usuario revisa y aprueba ese identificador.
5. `apply` rechaza el plan si el repositorio cambió y escribe únicamente las operaciones aprobadas.
6. `doctor` valida la estructura instalada. `check` ejecuta los comandos configurados y guarda un recibo local.

Sam Harness conserva el contenido existente de `AGENTS.md`, `CLAUDE.md`, `GEMINI.md`, las instrucciones de Copilot, `.gitignore` y GitLab CI mediante bloques delimitados. No concede autoridad para commit, push, release o deploy.

## Comandos

```text
sam-harness scan [path] [--format human|json]
sam-harness plan [path] --profile auto|baseline|production|regulated [--answers archivo]
sam-harness apply --plan <archivo> --accept <plan-id>
sam-harness check [path] [--format human|json]
sam-harness doctor [path]
sam-harness upgrade [path] --to <versión>
```

Los planes se guardan por defecto en el directorio temporal del sistema operativo. `--output` solo acepta un archivo nuevo fuera del repositorio. Se rechazan archivos existentes y rutas internas. `scan` y `plan` no cambian archivos versionados.

## Perfiles

`baseline` instala el contrato del repositorio, los límites de autoridad, los gates locales, las reglas de evidencia y los controles de calidad para interfaces.

`production` añade CI, runbooks de release y rollback, artefactos inmutables, SBOM, procedencia, reconciliación de migraciones y observación en producción.

Esos controles declaran requisitos, no pruebas. La promoción todavía exige comprobantes de la ejecución real de CI, el digest del artefacto, SBOM, procedencia, aprobaciones y observación del sistema en producción.

`regulated` añade threat model, gobierno de datos, separación de aprobaciones, evidencia de auditoría, ejercicios de recuperación y retiro. El perfil no certifica cumplimiento regulatorio.

## Stacks compatibles

La primera versión detecta TypeScript y JavaScript, Python, Go y Rust, incluidos los monorepos mixtos. La integración con GitHub Actions y GitLab CI entra en el plan solo cuando el usuario autoriza cambios en CI.

## Desarrollo

El proyecto usa Go 1.27.

```bash
go test ./...
go test -race ./...
go vet ./...
go build ./cmd/sam-harness
python3 scripts/validate-skill.py skills/sam-harness
```

La [matriz de trazabilidad](book-traceability.md) conecta los 20 capítulos del libro con controles, preguntas, plantillas o pruebas. CI falla si algún capítulo queda sin representación.

## Seguridad y licencia

Consulta [SECURITY.md](../SECURITY.md) para informar de vulnerabilidades. Las releases incluyen checksums, una firma keyless de Cosign, SBOMs CycloneDX y procedencia de build de GitHub.

Licencia MIT. Copyright 2026 Samuel Fajreldines.
