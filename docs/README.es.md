# sam-harness

[English](../README.md) | [Português](README.pt-BR.md)

Sam Harness convierte la arquitectura, los comandos, el recorrido hasta producción y los riesgos reales de un repositorio en instrucciones duraderas y en un ciclo de desarrollo ejecutable para agentes de programación. No inserta un prompt enorme en cada conversación. Una skill portátil guía la adopción y la operación, un CLI escrito en Go crea planes y recibos deterministas, y los archivos instalados mantienen las reglas después de cerrar el chat.

El método procede del libro [Development Harness](https://www.samuelfaj.com/books/development-harness-course/development-harness-course.es.html), de Samuel Fajreldines.

[![Flujo de Development Harness, desde las reglas del repositorio hasta producción segura](../assets/sam-harness.png)](https://www.samuelfaj.com/books/development-harness-course/development-harness-course.es.html)

Lee el libro: [🇪🇸 Español](https://www.samuelfaj.com/books/development-harness-course/development-harness-course.es.html) · [🇺🇸 English](https://www.samuelfaj.com/books/development-harness-course/development-harness-course.en-US.html) · [🇧🇷 Português](https://www.samuelfaj.com/books/development-harness-course/development-harness-course.html)

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
Usa $sam-harness y aplica el harness a este repositorio.
```

```text
/sam-harness apply
```

La skill pide permiso antes de descargar el CLI. Los scripts de instalación exigen Cosign y comprueban el certificado de la release y el checksum antes de instalar el binario en la caché del usuario.

La petición equivalente funciona en los tres idiomas compatibles:

```text
Aplica sam-harness aquí.
Apply sam-harness here.
Aplique o sam-harness aqui.
```

La URL del repositorio basta para que Codex o Claude Code descubran la skill portátil, el CLI verificado y el contrato de adopción. Prefiere la skill `$sam-harness` ya instalada; si no, sigue el mismo camino guiado del CLI. No descargues el CLI hasta que el operador lo pida; después verifica checksum, firma y versión.

```text
Ayúdame a implementar aquí https://github.com/samuelfaj/sam-harness
Pregunta lo que el repositorio no demuestra, adáptalo al proyecto, implementa los controles que falten en etapas aprobadas y termina con evidencia.
```

```text
Help me completely implement https://github.com/samuelfaj/sam-harness in this repository.
Ask me what cannot be inferred, adapt it to this project, implement missing controls in approved stages, and finish with evidence.
```

```text
Me ajude a implementar aqui o https://github.com/samuelfaj/sam-harness
Pergunte o que o repositório não prova, adapte ao projeto, implemente os controles que faltam em etapas aprovadas e termine com evidência.
```

Esos pedidos ejecutan `sam-harness onboard`, `sam-harness adopt --auto` o `sam-harness adopt --guided`. El agente pregunta solo lo que el árbol no demuestra, escribe un archivo de respuestas reutilizable sin valores de credencial, muestra el ID del plan, los archivos, la autoridad y los gates antes de cualquier escritura, y aplica solo con `--accept` de ese ID.

## Cómo funciona

1. `scan` lee manifiestos, comandos, workspaces, CI, estado de Git e indicios de interfaz, persistencia y despliegue. Registra comandos literales de jobs de GitLab y GitHub con sus directorios efectivos; una coincidencia exacta ya cubierta por la CI del cliente se muestra en el plan y se omite de los gates generados por Harness. Los comandos dinámicos o no equivalentes siguen siendo obligatorios. No modifica el repositorio.
2. El agente pregunta por los hechos de negocio que el código no puede demostrar, como criticidad, sensibilidad de datos, uso en producción, autoridad, diseño, rollback, responsables, comandos ambiguos y un proveedor de CI no detectado.
3. `plan` recomienda `baseline`, `production` o `regulated` y enumera las operaciones bajo un identificador criptográfico que caduca después de 30 minutos.
4. El usuario revisa y aprueba ese identificador.
5. `apply` rechaza el plan si el repositorio cambió y escribe únicamente las operaciones aprobadas.
6. `doctor` valida la estructura instalada. `check` ejecuta los controles locales configurados y guarda un recibo de evidencia.
7. `pipeline` ejecuta una fase aprobada — análisis estático, pruebas, revisión pre-merge con seis roles, artefacto, staging, producción, observación, rollback o migración — y guarda un recibo específico de la fase.
8. Si `static`, `test`, `review` o `artifact` falla y la corrección se habilitó explícitamente, `repair` valida el recibo actual, ejecuta el comando configurado en un sandbox Git aislado, aplica los límites acumulativos de intentos/archivos/líneas, repite `static` y `test` y emite un parche solo de la corrección con su SHA-256. Los recibos de revisión fallida incluyen un manifiesto de reparación con hash que contiene el cambio exacto y la aceptación observable de cada revisor, para corregir todo el trabajo conocido junto antes de la revisión independiente.

Sam Harness conserva el contenido existente de `AGENTS.md`, `CLAUDE.md`, `GEMINI.md`, las instrucciones de Copilot, `.gitignore` y GitLab CI mediante bloques delimitados. Añade guías de workflow, revisores, presupuesto de cambios, observación y retiro sin sustituir contenido del usuario.

## Comandos

```text
sam-harness scan [path] [--format human|json]
sam-harness plan [path] --profile auto|baseline|production|regulated [--answers archivo]
sam-harness apply --plan <archivo> --accept <plan-id>
sam-harness onboard [path] [--answers archivo] [--answers-output archivo] [--locale en-US|pt-BR|es] [--accept plan-id] [--output archivo] [--format human|json] [--interactive true|false]
sam-harness adopt --auto [path] [--answers archivo] [--answers-output archivo] [--locale en-US|pt-BR|es] [--accept plan-id] [--output archivo] [--format human|json]
sam-harness adopt --guided [path] [--answers archivo] [--answers-output archivo] [--locale en-US|pt-BR|es] [--accept plan-id] [--implement control] [--waiver-control id --waiver-risk texto --waiver-reason texto] [--output archivo] [--format human|json]
sam-harness bootstrap github [path] [--accept plan-id] [--format human|json]
sam-harness bootstrap gitlab [path] [--accept plan-id] [--format human|json]
sam-harness stage classifier|context|planning|implementation|review|repair --input archivo [--format human|json]
sam-harness freeze check [path] [--policy archivo] [--now rfc3339] [--exception archivo] [--head sha] [--base sha] [--branch nombre] [--kind feature] [--scheduled-release true|false]
sam-harness check [path] [--format human|json] [--receipt true|false]
sam-harness doctor [path]
sam-harness upgrade [path] --to <versión> [--answers <archivo>] [--output <archivo>]
sam-harness pipeline [path] [--config <archivo-absoluto-o-contenido>] [--review-base <directorio-absoluto> --review-base-sha <hex> --review-head-sha <hex>] --phase <static|test|review|artifact|staging|production|observe|rollback|migration|all> [--receipt true|false]
sam-harness repair [path] [--config <archivo-absoluto-o-contenido>] --receipt <archivo> [--receipt-output true|false]
```

Los planes se guardan por defecto en el directorio temporal del sistema operativo. `--output` solo acepta un archivo nuevo fuera del repositorio. Se rechazan archivos existentes y rutas internas. `scan` y `plan` no cambian archivos versionados.

## Ejemplo completo del ciclo

Primero inspecciona el repositorio y prepara el plan. El archivo de respuestas se mantiene fuera del repositorio y registra los comandos y responsables reales que el código no puede demostrar:

Para un plan `production` o `regulated`, usa la [estructura de configuración del workflow](../skills/sam-harness/references/workflow-configuration.md) y sustituye todos los valores de ejemplo por comandos aprobados del repositorio o proveedor.

```bash
sam-harness scan /ruta/al/repositorio --format json
sam-harness plan /ruta/al/repositorio --profile auto --answers /tmp/sam-harness-respuestas.json
```

Revisa la justificación del perfil, las decisiones pendientes, todas las operaciones de archivos, la caducidad y el ID del plan. Solo después de aprobar ese ID exacto:

```bash
sam-harness apply --plan /tmp/sam-harness-plan.json --accept <id-del-plan>
sam-harness doctor /ruta/al/repositorio
sam-harness check /ruta/al/repositorio --receipt true
```

En un ciclo de producción, ejecuta cada frontera por separado para mantener visible su estado. Una revisión pre-merge también necesita un checkout confiable y separado de la base propuesta, además de los SHA exactos de base y head. Antes de staging, migración, producción o rollback, confirma la autoridad para ese efecto externo:

```bash
sam-harness pipeline /ruta/al/repositorio --phase static --receipt true
sam-harness pipeline /ruta/al/repositorio --phase test --receipt true
sam-harness pipeline /ruta/al/repositorio --review-base /ruta/absoluta/a/la/base-confiable --review-base-sha <sha-base> --review-head-sha <sha-head> --phase review --receipt true
sam-harness pipeline /ruta/al/repositorio --phase artifact --receipt true
sam-harness pipeline /ruta/al/repositorio --phase staging --receipt true
sam-harness pipeline /ruta/al/repositorio --phase migration --receipt true
sam-harness pipeline /ruta/al/repositorio --phase production --receipt true
sam-harness pipeline /ruta/al/repositorio --phase observe --receipt true
```

`--review-base`, `--review-base-sha` y `--review-head-sha` solo son válidos con `review` o `all`; los flags de SHA deben aparecer juntos y requieren el directorio base. La revisión con secretos exige los tres. Los SHA hexadecimales de 40 o 64 caracteres deben coincidir con el `HEAD` de los checkouts base y objetivo antes y después de la revisión. El recibo registra `review_base_root`, `review_base_sha`, `review_base_fingerprint`, `review_head_sha`, `review_head_fingerprint`, el artefacto `review_patch` canónico y `review_patch_sha256`; los prompts de los revisores solo llevan `review_patch_path` y `review_patch_sha256`, para que lean el diff no confiable desde el sandbox aislado sin incrustar sus bytes. Cualquier cambio de identidad o contenido bloquea. Una revisión local solo del head, sin `--review-base`, puede seguir siendo útil, pero no satisface el gate pre-merge obligatorio sobre el delta.

La convergencia de la revisión sigue una política fija: la pasada inicial revisa el diff completo del MR entre base y head y congela su manifiesto; las pasadas posteriores revalidan esos hallazgos congelados mediante los ID del mismo revisor; solo un nuevo P0/P1 probado en el delta de corrección entre el head anterior y el actual puede entrar en el registro bloqueante. Los nuevos P2/P3 y los problemas preexistentes no relacionados no inician otro ciclo de corrección. Los hallazgos iniciales y las nuevas regresiones P0/P1 deben señalar una línea añadida o modificada en el parche aplicable, o la línea `0` solo para evidencia de archivo con solo eliminaciones, archivo eliminado o renombrado puro. La falta de prueba bloquea. Cada recibo JSON tiene a su lado un HTML independiente y escapado.

`--phase all` es útil en una automatización ya aprobada, pero no elimina la protección del entorno de producción ni la aprobación manual; proporciona el directorio base y el par de SHA cuando `all` deba satisfacer una revisión pre-merge con secretos del proveedor. Rollback nunca forma parte de `all` ni comienza automáticamente después de un fallo; usa su entrada manual independiente solo cuando correspondan el runbook y la autoridad:

```bash
sam-harness pipeline /ruta/al/repositorio --phase rollback --receipt true
```

Si un recibo indica fallo y la corrección limitada está habilitada:

```bash
sam-harness repair /ruta/al/repositorio --receipt .sam-harness/evidence/<recibo-fallido>.json --receipt-output true
```

La reparación acepta únicamente un recibo producido por la versión actual, fallido o bloqueado, de `static`, `test`, `review` o `artifact` de este repositorio. Un recibo de revisión fallida debe contener un manifiesto de reparación íntegro, sin conflictos y vinculado a las mismas identidades, fingerprints, digest del parche y hallazgos. El prompt exige verificar independientemente y aplicar todas las acciones del manifiesto en un delta coherente, sin detenerse en el primer elemento ni aplazar trabajo conocido. La reparación automática de revisión se limita a una pasada: la rama resultante recibe una revisión independiente, pero no puede generar otra rama automática de reparación. Un recibo transportado entre workspaces limpios de CI solo se vuelve a vincular después de que la identidad del repositorio, el digest de configuración y los fingerprints inicial y final coincidan con el destino. Envía un prompt estructurado que contiene el recibo no confiable — no el recibo bruto como instrucción — a un sandbox Git temporal y autónomo, sin remotos Git, hooks del repositorio ni credenciales Git heredadas. Su entorno de proceso limpio expone solo los secretos configurados con scope `repair`. Antes de verificar o aplicar nada, Sam Harness bloquea si un secreto expuesto a la corrección aparece en nuevos bytes de un archivo regular, en el destino modificado de un symlink o en una línea añadida al parche; una aparición sin cambios que ya estaba en el baseline no se trata como una filtración nueva. El escape mediante symlink, los cambios en los controles de Git, el estado obsoleto del destino y el exceso de presupuesto también bloquean el flujo. Un delta exitoso debe superar nuevos `static` y `test` antes de aplicarse al destino todavía intacto. La revisión independiente sigue siendo obligatoria. El recibo registra el parche solo de la corrección y `repair_patch_sha256`.

Si la publicación de change requests está habilitada y autorizada por separado, CI envía ese par parche/recibo como datos no confiables a un publicador separado por credenciales. Exige exactamente un parche y un recibo, comprueba su nombre y SHA-256, deshabilita hooks, aplica solo ese parche en la rama de reparación configurada y abre un PR o MR. Nunca envía directamente a una rama protegida.

Para actualizar una instalación de producción heredada, proporciona las decisiones obligatorias del workflow de la versión actual en un archivo de respuestas:

```bash
sam-harness upgrade /ruta/al/repositorio --to 0.8.4 --answers /tmp/sam-harness-respuestas-v0.8.json
```

`upgrade` combina las respuestas explícitas con la configuración instalada y produce un plan con caducidad; no lo aplica. Revisa las decisiones pendientes y todas las operaciones, después aprueba y aplica el nuevo ID exacto. Usa la [estructura de configuración del workflow](../skills/sam-harness/references/workflow-configuration.md) para la cobertura de guards `static`/`test`, las decisiones sobre nombres de secretos, entorno protegido de agentes y control plane del proveedor, las atestaciones del filesystem y del comando confiable, y los comandos del ciclo que una configuración heredada de producción v0.1 no contiene.

## Fronteras de confianza

- `scan` y `plan` no editan archivos fuente versionados. `pipeline` orquesta comandos configurados y recibos en vez de editar el código directamente, pero esos comandos pueden modificar el repositorio o sistemas externos. `apply` escribe únicamente un plan no caducado, aprobado explícitamente y cuyo fingerprint aún coincide con el repositorio.
- Un comando configurado es una política ejecutable, no un permiso permanente. El usuario debe autorizar la acción remota, deploy, rollback, migración, cambio de credenciales o acción irreversible actual.
- Sam Harness calcula el fingerprint antes y después de `static` y `test` y bloquea la fase si esos comandos modifican el repositorio. El bloqueo no restaura el árbol. Esto detecta mutaciones del repositorio; no aísla efectos externos. Cada comando externo configurado sigue siendo la frontera de ejecución aprobada por el usuario.
- En runtime, `review` y `repair` requieren autoridad de red. Staging, migración y observación requieren red y deploy; producción y rollback requieren red, deploy y release.
- La revisión es un gate pre-merge sobre los SHA y fingerprints exactos de la base confiable y del head propuesto, además del hash del parche canónico. `ci_secret_bindings` almacena solo el scope, el nombre de la variable de entorno y el nombre del secreto en GitHub/GitLab; `review` y `repair` usan scopes separados. El campo de respuestas `agent_secret_environments`, instalado como `ci.agent_secret_environments`, nombra el entorno protegido del proveedor donde deben residir esos secretos de agentes; es distinto del entorno de release de producción. El campo de respuestas `agent_control_planes`, instalado como `ci.agent_control_planes`, define el status check obligatorio y los nombres de credenciales de la GitHub App dedicada o el proyecto externo de GitLab. Sam Harness nunca serializa valores secretos en archivos versionados.
- Los jobs comunes de `pull_request`/`merge_group` en GitHub y los jobs de merge request en GitLab no reciben secretos vinculados a agentes. Para un scope vinculado en GitHub, `.github/workflows/sam-harness-agents.yml`, cargado desde la rama predeterminada, usa `pull_request_target`, un `repository_dispatch` llamado `sam_harness_merge_group_review` o `workflow_run` de una ejecución fallida. Nunca escucha `merge_group` directamente: una App o webhook externo debe enviar como datos el SHA exacto del head de la cola, el SHA actual de la rama predeterminada y la ref `gh-readonly-queue` del proveedor. El resolver vuelve a consultar esas refs antes del review y de concluir el check; un dispatch ausente o una identidad distinta bloquea el check obligatorio. El workflow usa la versión publicada del CLI declarada por la configuración confiable y la configuración de la base confiable, limita los secretos del modelo al step de review o repair correspondiente y nunca ejecuta setup, hooks, caché, actions locales ni comandos del repositorio objetivo. Nunca publica reparación automática para un merge group. En GitHub, los bindings mixtos de review/repair mueven solo el scope vinculado y conservan localmente el trabajo sin credenciales aplicable.
- GitLab no genera un loop de agentes dentro del repositorio objetivo. Con `mode: external`, el proyecto externo configurado asume todo el review, corrección y publicación por agentes, independientemente de los bindings, y debe publicar el estado obligatorio configurado para el head exacto y actual del MR. El pipeline del MR omite todos los jobs locales de review, corrección y publicación por agentes; la ausencia del estado externo bloquea el merge. Define `gitlab_external_pipeline_policy: true` solo cuando una GitLab Pipeline Execution Policy protegida también controle los gates de static, test y artifact; los jobs locales generados siguen activos en ramas confiables, pero quedan deshabilitados en merge requests.
- Crear credenciales y configurar el control plane siguen siendo tareas administrativas externas. Restringe el entorno de agentes de GitHub a ramas predeterminadas/protegidas, exige reviewers humanos, habilita prevent-self-review, guarda el ID y la clave privada de la App dedicada solo en ese entorno, exige su status check exacto y vuelve a leer toda la configuración. Configura y verifica en GitLab el proyecto externo, variables/entorno protegidos, status check, rama protegida y reglas de aprobación. Un fork, secreto no disponible, check ausente, configuración base ausente, runtime publicado ausente o cambio del head bloquean; nunca omitas ni rebajes el gate silenciosamente. Por eso, el workflow generado para el propio Sam Harness debe establecer la release correspondiente y la configuración base mediante un bootstrap confiable y aprobado antes de que los agentes reciban secretos. Usa `ci_secret_waivers` solo cuando los comandos aprobados realmente no necesiten credenciales.
- Cada reviewer requiere una atestación `filesystem_read_only: true` verificada por el usuario, y la corrección habilitada requiere `filesystem_sandboxed: true`. Los secretos `review` vinculados al proveedor también exigen `trusted_external_command: true` en los seis reviewers; un secreto `repair` vinculado exige lo mismo en la corrección. El ejecutable debe resolverse fuera del repositorio objetivo. `trusted_config_arguments` enumera posiciones reales y únicas del argv, con índice zero-based mayor que cero, para helpers seguros resueltos desde el directorio de la configuración confiable; las entradas no listadas que parezcan rutas del objetivo bloquean. Los comandos locales y sin bindings, cubiertos solo por waiver, pueden seguir siendo relativos al repositorio.
- Por ejemplo, el argv de Codex puede usar `codex exec --sandbox read-only -` para la revisión y `codex exec --sandbox workspace-write -` para la corrección. Comprueba la herramienta instalada, el ejecutable externo, los helpers indexados y el contrato de salida JSON exacta antes de atestarlo: el argv arbitrario y su sandbox siguen dentro de la base de computación confiable, y Sam Harness no es un sandbox genérico del sistema operativo. Se permite un paquete `npx` con versión exacta bajo la forma más estricta documentada en la referencia del workflow; no se permite package dispatch sin versión fija.
- Los seis revisores son `architecture`, `security`, `correctness`, `test_quality`, `business_rules` y `simplicity`. Sus comandos deben configurarse y atestiguarse de forma independiente como de solo lectura en el filesystem. Cada uno devuelve `review_complete: true` y todo hallazgo accionable con `required_change` exacto y `acceptance` observable. El prompt pre-merge solo lleva `review_patch_path` y `review_patch_sha256`; el recibo vincula el fingerprint de la base, el fingerprint del head, el parche canónico, su SHA-256 y un manifiesto de reparación con hash que contiene los hallazgos dentro del diff. JSON inválido o incompleto, un hallazgo P0/P1 dentro o fuera del diff, una mutación del repositorio o un cambio de base/head bloquean la revisión. Las sugerencias P2/P3 fuera del diff se excluyen de la corrección y siguen visibles en el recibo HTML.
- La fase de artefacto compila una sola vez y registra las rutas y SHA-256 del artefacto, SBOM y procedencia, además del fingerprint del código fuente. Staging y producción vuelven a comprobarlo todo y promueven el mismo artefacto sin recompilar.
- Un recibo demuestra únicamente su propia fase y fingerprint. Una edición no es un control aprobado; un control aprobado no es CI; un deploy no es una ventana de observación saludable.

## Cobertura ejecutable y contexto local de los agentes

La planificación `production` y `regulated` exige que cada categoría de `static_guards` y `test_guards` tenga exactamente una entrada en `commands` o una exención aprobada y no vacía en `waivers`:

- Estática: `format`, `lint`, `typecheck`, `architecture`, `security`, `dependencies`, `schema`, `project_policies`.
- Prueba: `unit`, `integration`, `contract`, `business_invariants`, `property`, `mutation`, `e2e`, `performance`.

Las fases `static` y `test` ejecutan tanto los controles descubiertos en el repositorio como los comandos configurados por categoría. Una exención es evidencia auditable de un elemento omitido, no un control aprobado. Las categorías ausentes bloquean el plan en vez de recibir comandos inventados.

La aplicación también instala `.agents/skills/sam-harness-<lifecycle>/SKILL.md` para `classify`, `context`, `plan`, `implement`, `review`, `repair` y `release`; contratos `<workspace>/AGENTS.md` gestionados para workspaces no raíz detectados; y `.github/pull_request_template.md` junto con `.gitlab/merge_request_templates/sam-harness.md` con la escala de evidencia y el checklist de UX orientado a personas. Carga solo la skill local del estado actual y sigue el `AGENTS.md` más cercano. Las plantillas organizan afirmaciones; no las demuestran.

## Perfiles

`baseline` instala el contrato del repositorio, los límites de autoridad, los gates locales, las reglas de evidencia y los controles de calidad para interfaces.

`production` añade CI, bindings de nombres de secretos por scope o waivers explícitos, declaraciones de entornos protegidos y control planes de agentes, fronteras de comando externo confiable para revisión y reparación con secretos del proveedor, seis roles fijos de revisión, corrección limitada en sandbox y publicador de parches separado, comandos inmutables de artefacto/SBOM/procedencia, staging y promoción protegida a producción, rollback manual, controles de salud y observación, comandos de migración, porcentajes de canary y un calendario de releases.

Esos controles declaran requisitos, no pruebas. La promoción todavía exige comprobantes de la ejecución real de CI, el digest del artefacto, SBOM, procedencia, aprobaciones y observación del sistema en producción.

`regulated` añade threat model, gobierno de datos, separación de aprobaciones, evidencia de auditoría, ejercicios de recuperación y retiro. El perfil no certifica cumplimiento regulatorio.

Los planes `production` y `regulated` no pueden aplicarse mientras falte una decisión ejecutable obligatoria del workflow. `baseline` puede omitir controles de entrega remota.

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
