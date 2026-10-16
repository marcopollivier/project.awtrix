# project.awtrix

Alimenta um **Ulanzi TC001** (matriz de LEDs 32×8) rodando **[AWTRIX NG](https://blueforcer.github.io/awtrix-ng/)** com:

- contadores de **YouTube**, **GitHub**, **Instagram** e **LinkedIn**, com variação do dia, barra até a próxima meta e comemoração quando bate a meta;
- o **Vasco ao vivo** (só pelo hub): próximo jogo, placar em tempo real, notificação (com som) de gol, intervalo e fim;
- um **simulador no navegador**, que fala a mesma API do relógio, para ver tudo antes de mandar para o aparelho.

Tem duas partes, e só a primeira é obrigatória:

```
relógio (AWTRIX NG)                         hub (Go, opcional, no Mac)
  scripts/*.ax ── busca direto ──► APIs       sources ─► scheduler ─► simulador / apps empurrados
       │                                         │
       └──── se o hub estiver no ar ────► GET /api/v1/metrics/{key}
```

- **Relógio independente**: os scripts em `scripts/` rodam dentro do relógio, buscam os dados sozinhos e guardam a variação do dia e as metas no próprio aparelho. Não precisa de nada ligado em casa.
- **Hub** (`awtrix-hub`): instala e configura os scripts (`deploy`), roda o simulador e, quando está no ar, serve os números para os scripts. Se ele cair, os scripts voltam a buscar direto.

## Começando

```sh
cp config.example.yaml config.yaml
cp .env.example .env          # preencha as chaves que tiver
make dev                      # hub + simulador → http://localhost:7070
```

Para o relógio funcionar sozinho: instale o AWTRIX NG (veja [Instalando o AWTRIX NG](#instalando-o-awtrix-ng-no-relógio)), coloque o IP em `device.url` e rode `make build && ./bin/awtrix-hub deploy`.

| Comando | O que faz |
|---|---|
| `make sim` | só o simulador (`awtrix-hub sim`) |
| `make dev` | hub + simulador no mesmo processo (`run --sim`) |
| `make run` | hub enviando para os `targets` da config |
| `make preview` | consulta cada fonte uma vez, envia e sai |
| `make mock` | **tudo com dados de mentira**: contadores subindo, metas, jogo do Vasco; sem chaves |
| `make replay` | replay de um jogo do Vasco gravado (gols, intervalo, fim) no simulador |
| `awtrix-hub icons` | envia os ícones da config para o relógio (o `run` também faz isso ao iniciar) |
| `awtrix-hub set linkedin 2890` | atualiza o contador manual do LinkedIn (e o script do relógio, se `device.url` estiver definido) |
| `awtrix-hub deploy` | instala `scripts/*.ax` no relógio (AWTRIX NG) e preenche as configurações a partir da config e do `.env` |
| `awtrix-hub run --only github --debug` | roda só uma fonte, com logs detalhados |
| `make test` | testes |
| `make test-scripts` | testa `scripts/*.ax` num interpretador [Berry](https://github.com/berry-lang/berry) com imitações do runtime do NG (`BERRY=/caminho/para/berry`) |

Estado (variação do dia, metas já comemoradas, último placar) fica em `~/.awtrix-hub/`.

Com `api.addr` definido (ex.: `0.0.0.0:7080`), o `run` também serve `GET /api/v1/metrics` com o último valor de cada contador. É daí que os scripts do relógio leem quando o hub está no ar.

## O simulador

`http://localhost:7070` mostra a matriz com LEDs, ícones (inclusive animados), rolagem de texto, barra de progresso, arco-íris e notificações por cima do carrossel. Tem também:

- **Carrossel**: apps ativos e há quanto tempo cada um foi atualizado;
- **Payload em exibição**: o JSON exato que iria para o relógio;
- **Enviar payload**: teste qualquer JSON da [API do AWTRIX NG](https://blueforcer.github.io/awtrix-ng/esp32/reference/payload/) (nome vazio = notificação);
- **Som**: marque a caixa para ouvir os toques RTTTL (o buzzer do relógio).

Ele aceita os mesmos endpoints do relógio com [AWTRIX NG](https://blueforcer.github.io/awtrix-ng/esp32/reference/http/), então dá para testar com `curl`. Como no relógio, exige `Content-Type: application/json` (sem ele: `415`) e recusa chaves desconhecidas, como as do AWTRIX 3 (`422`):

```sh
curl -X PUT -H 'Content-Type: application/json' localhost:7070/api/v1/apps/pushed/teste -d '{"text":"OLA","icon":"2799"}'
curl -X POST -H 'Content-Type: application/json' localhost:7070/api/v1/notifications -d '{"text":"GOOOL","textColor":"palette","palette":"Rainbow","repeat":1}'
curl -X DELETE localhost:7070/api/v1/apps/teste
```

A fonte e o tempo de rolagem são aproximações do relógio. Para ajustar, compare lado a lado com o relógio: empurre o mesmo app para os dois (coloque os dois em `targets`).

## Relógio independente (scripts)

Cada contador é um script [Berry](https://berry-lang.github.io/) que o relógio roda sozinho:

| Script | De onde vem | Precisa de |
|---|---|---|
| `counter.ax` | módulo com a base de todos: formatação, variação do dia, metas, fallback | — |
| `github.ax` | `api.github.com/users/{user}` | nada (token opcional) |
| `github_stars.ax` | GraphQL do GitHub | **token** só leitura (a lista REST de repositórios tem ~350 KB, e o relógio aceita respostas de até 8 KB). Sem token, só pelo hub |
| `youtube.ax` | YouTube Data API v3 | chave da API |
| `instagram.ax` | `graph.instagram.com` | token (renovado toda semana pelo próprio script) |
| `linkedin.ax` | valor digitado | `awtrix-hub set linkedin N` ou a interface do relógio |

`awtrix-hub deploy`:
1. envia os ícones;
2. instala os scripts;
3. preenche as configurações de cada um a partir de `config.yaml` e `.env` (usuário, chaves, metas, cores, endereço do hub);
4. mostra o erro de cada script, com a linha, se houver.

Rodar de novo é seguro: tokens vazios no `.env` não apagam o que foi digitado no relógio. Depois do deploy, tudo pode ser ajustado em *Apps → ⚙* na interface do relógio.

### Fallback entre hub e busca direta

Cada app tem a opção **Fonte**, escolhida no relógio:

| Fonte | Comportamento |
|---|---|
| `auto` (padrão) | Com o **Hub** preenchido no módulo `counter`, pergunta ao hub. Se ele não responde ou o valor tem mais de 6h, marca o hub como fora por 30 min e busca direto **no mesmo ciclo**. Sem hub configurado, sempre direto |
| `direct` | só busca direto |
| `hub` | só o hub. Útil para as estrelas sem token |

O estado (último valor, variação do dia, metas já comemoradas) fica no relógio, então trocar de fonte não zera nada. Se nenhuma fonte responder, o último valor continua na tela e fica cinza depois de 6h. Para o hub servir os números, defina `api.addr: 0.0.0.0:7080` e `api.public_url: http://<ip-do-mac>:7080` e rode `deploy` de novo.

### Segurança

O AWTRIX NG não confere o certificado HTTPS, e os scripts (com os tokens) podem ser lidos pela API do relógio. Por isso:
- use **tokens só de leitura**: GitHub fine-grained sem permissões além de público, chave do YouTube restrita à Data API;
- ligue a **senha da interface** (`authEnabled`) e coloque `AWTRIX_USER`/`AWTRIX_PASS` no `.env`.

## Fontes

| Fonte | Precisa de | Observações |
|---|---|---|
| GitHub | nada (`GITHUB_TOKEN` opcional) | seguidores e soma de ⭐ dos repositórios próprios (sem forks) |
| YouTube | `YOUTUBE_API_KEY` | inscritos vêm **arredondados** pela API pública (3 dígitos significativos); views do último vídeo nas primeiras 48h |
| Instagram | `INSTAGRAM_TOKEN` | conta profissional, login do Instagram; o hub renova o token sozinho |
| LinkedIn | nada | sem API para perfil pessoal: valor manual via `set` |
| Futebol | nada | API não oficial da ESPN; Vasco = time `3454`; consulta a cada 30s só durante o jogo |

### Chave do YouTube

1. [console.cloud.google.com](https://console.cloud.google.com) → crie um projeto.
2. *APIs e serviços → Biblioteca* → ative a **YouTube Data API v3**.
3. *Credenciais → Criar credenciais → Chave de API* (restrinja à YouTube Data API v3).
4. Coloque em `.env`: `YOUTUBE_API_KEY=...`

Custo: cerca de 3 unidades por consulta, de uma cota diária de 10.000.

### Token do Instagram

Usa a **API do Instagram com login do Instagram** (`graph.instagram.com`), que não precisa de Página do Facebook.

1. A conta precisa ser **profissional** (Criador ou Empresa).
2. Em [developers.facebook.com](https://developers.facebook.com) → *My Apps* → *Create app*, escolha o caso de uso do Instagram (gerenciar mensagens e conteúdo) e o tipo *Business*.
3. No painel do app: *Instagram* → *API setup with Instagram login* → *Generate access tokens* → *Add account*, entre com a conta e autorize (`instagram_business_basic`).
4. Se a Meta pedir, aceite o convite de testador no app do Instagram (*Configurações → Apps e sites → Convites de testador*).
5. Clique em *Generate token*, copie o token de longa duração e coloque no `.env`: `INSTAGRAM_TOKEN=...`

O token vale 60 dias. O hub renova o token toda semana e guarda o novo em `~/.awtrix-hub/instagram_token.json`, então o do `.env` só serve de ponto de partida. Trocar o token no `.env` faz o hub recomeçar a partir dele. Se o token ficar inválido, o relógio mostra `TOKEN INSTAGRAM INVALIDO` uma vez por dia.

### Futebol

`leagues` define em quais competições procurar o próximo jogo (Brasileirão `bra.1`, Copa do Brasil `bra.copa_do_brazil`, Sul-Americana `conmebol.sudamericana`…). O hub pega o jogo mais próximo entre elas.

| Fase | Quando | Consulta | Tela |
|---|---|---|---|
| idle | jogo em outro dia | 1h | `SAB 18H REM` |
| matchday | jogo hoje | 10 min | `HOJE 18H REM` |
| pre | até 15 min antes | 1 min | `REM EM 12M` |
| live | em andamento | 30s | `VAS 1x0 REM 67'` |
| post | até 2h depois | 1h | placar final em verde, amarelo ou vermelho |

O último placar fica salvo, então reiniciar o hub no meio do jogo não repete notificação de gol.

## Instalando o AWTRIX NG no relógio

> Substitui o firmware atual (da Ulanzi ou o AWTRIX 3) e apaga as configurações dele. O backup do firmware de fábrica da Ulanzi fica em `~/.awtrix-hub/backup/`.

O cabo só é necessário nesta instalação. Depois, scripts, configurações e atualizações do NG vão pelo Wi-Fi.

1. Use um cabo USB-C **de dados** (cabo só de carga não funciona) e ligue o relógio direto no Mac.
2. No **Chrome ou Edge** (o Safari não tem Web Serial), abra o [flasher do AWTRIX NG](https://blueforcer.github.io/awtrix-ng/esp32/getting-started/flashing/), escolha **Fresh install** e a porta `usbserial`/`wchusbserial`. Espere aparecer *Done*.
3. O relógio abre a rede Wi-Fi de configuração dele. Conecte-se, configure o Wi-Fi de casa e anote o IP, que aparece no display a cada inicialização.
4. Na interface web (`http://<ip>`):
   - fuso `<-03>3` e NTP `br.pool.ntp.org`;
   - deixe só o app de hora entre os nativos;
   - ajuste o brilho;
   - ligue a senha da interface.
5. Na `config.yaml`: `device.url: http://<ip>`. No `.env`: `AWTRIX_USER`/`AWTRIX_PASS`.
6. `make build && ./bin/awtrix-hub deploy`.
7. Se a porta não aparecer no passo 2, rode `ls /dev/cu.*`. Quase sempre o problema é o cabo.

Ícones: além dos IDs da LaMetric, a config aceita ícones próprios, um GIF 8×8 em `icons/` referenciado pelo nome (ex.: `stars: "gh_stars"`). O relógio não baixa ícones sozinho. O `deploy`, o `icons` e o `run` baixam os da LaMetric, convertem PNG para GIF e enviam para a pasta `/ICONS` do relógio.

## Rodando o hub sempre (launchd, opcional)

Não é necessário para o relógio funcionar. Serve para manter o endpoint de métricas e o futebol no ar enquanto o Mac estiver ligado.

```sh
make build
cp deploy/launchd/dev.omarco.awtrix-hub.plist ~/Library/LaunchAgents/
launchctl bootstrap gui/$(id -u) ~/Library/LaunchAgents/dev.omarco.awtrix-hub.plist
tail -f ~/Library/Logs/awtrix-hub.log
```

Quando o Mac dorme, o hub para. Os scripts do relógio percebem e voltam a buscar direto. Os apps empurrados pelo hub (futebol) usam `lifetimeMs`, então somem em vez de ficar mostrando dado velho.

## Ideias para depois

Clima e alerta de chuva · dólar/BTC · próxima reunião · CI e PRs · Spotify tocando · Pomodoro pelos botões · modo noturno · espectadores em live · views do vídeo novo do YouTube no script · MQTT/Home Assistant.
