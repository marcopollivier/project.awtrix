# project.awtrix

Alimenta um **Ulanzi TC001** (matriz de LEDs 32×8) rodando **[AWTRIX 3](https://blueforcer.github.io/awtrix3/)** com:

- contadores de **YouTube**, **GitHub**, **Instagram** e **LinkedIn**, com variação do dia, barra até a próxima meta e comemoração quando bate a meta;
- o **Vasco ao vivo**: próximo jogo, placar em tempo real, notificação (com som) de gol, intervalo e fim;
- um **simulador no navegador**, que fala a mesma API do relógio, para ver tudo antes de mandar para o aparelho.

```
sources (GitHub, YouTube, Instagram, LinkedIn, ESPN)
   └─► scheduler (variação do dia, metas, renderização)
          └─► awtrix.Client ──► simulador (localhost:7070) e/ou relógio (IP na rede)
```

## Começando

```sh
cp config.example.yaml config.yaml
cp .env.example .env          # preencha as chaves que tiver
make dev                      # hub + simulador → http://localhost:7070
```

| Comando | O que faz |
|---|---|
| `make sim` | só o simulador (`awtrix-hub sim`) |
| `make dev` | hub + simulador no mesmo processo (`run --sim`) |
| `make run` | hub enviando para os `targets` da config |
| `make preview` | consulta cada fonte uma vez, envia e sai |
| `make mock` | **tudo com dados de mentira**: contadores subindo, metas, jogo do Vasco; sem chaves |
| `make replay` | replay de um jogo do Vasco gravado (gols, intervalo, fim) no simulador |
| `awtrix-hub icons` | envia os ícones da config para o relógio (o `run` também faz isso ao iniciar) |
| `awtrix-hub set linkedin 2890` | atualiza o contador manual do LinkedIn |
| `awtrix-hub run --only github --debug` | roda só uma fonte, com logs detalhados |
| `make test` | testes |

Estado (variação do dia, metas já comemoradas, último placar) fica em `~/.awtrix-hub/`.

## O simulador

`http://localhost:7070` mostra a matriz com LEDs, ícones (inclusive animados), rolagem de texto, barra de progresso, arco-íris e notificações por cima do carrossel. Tem também:

- **Carrossel**: apps ativos e há quanto tempo cada um foi atualizado;
- **Payload em exibição**: o JSON exato que iria para o relógio;
- **Enviar payload**: teste qualquer JSON da [API do AWTRIX](https://blueforcer.github.io/awtrix3/#/api) (nome vazio = notificação);
- **Som**: marque a caixa para ouvir os toques RTTTL (o buzzer do relógio).

Ele aceita os mesmos endpoints do relógio, então dá para testar com `curl`. O relógio exige o cabeçalho `Content-Type: application/json`; sem ele, responde `ErrorParsingJson`:

```sh
curl -X POST -H 'Content-Type: application/json' 'localhost:7070/api/custom?name=teste' -d '{"text":"OLA","icon":"2799"}'
curl -X POST -H 'Content-Type: application/json' localhost:7070/api/notify -d '{"text":"GOOOL","rainbow":true,"repeat":1}'
```

A fonte e o tempo de rolagem são aproximações do relógio. Ajuste comparando lado a lado quando o aparelho estiver com AWTRIX 3 (coloque os dois em `targets`).

## Fontes

| Fonte | Precisa de | Observações |
|---|---|---|
| GitHub | nada (`GITHUB_TOKEN` opcional) | seguidores e soma de ⭐ dos repositórios próprios (sem forks) |
| YouTube | `YOUTUBE_API_KEY` | inscritos vêm **arredondados** pela API pública (3 dígitos significativos); views do último vídeo nas primeiras 48h |
| Instagram | `INSTAGRAM_USER_ID`, `INSTAGRAM_TOKEN` | conta profissional; avisa no relógio se o token expirar |
| LinkedIn | nada | sem API para perfil pessoal: valor manual via `set` |
| Futebol | nada | API não oficial da ESPN; Vasco = time `3454`; consulta a cada 30s só durante o jogo |

### Chave do YouTube

1. [console.cloud.google.com](https://console.cloud.google.com) → crie um projeto.
2. *APIs e serviços → Biblioteca* → ative a **YouTube Data API v3**.
3. *Credenciais → Criar credenciais → Chave de API* (restrinja à YouTube Data API v3).
4. Coloque em `.env`: `YOUTUBE_API_KEY=...`

Custo: cerca de 3 unidades por consulta, de uma cota diária de 10.000.

### Token do Instagram

1. A conta precisa ser **profissional** e estar vinculada a uma **Página do Facebook**.
2. [developers.facebook.com](https://developers.facebook.com) → crie um app (tipo *Business*) e adicione o produto **Instagram Graph API**.
3. No **Graph API Explorer**, gere um *User Token* com `instagram_basic`, `pages_show_list` e `pages_read_engagement`.
4. Troque por um token de longa duração e então pegue o **token da Página** (`GET /me/accounts`), que não expira.
5. `GET /{page-id}?fields=instagram_business_account` → este é o `INSTAGRAM_USER_ID`.
6. Coloque em `.env`: `INSTAGRAM_USER_ID=...` e `INSTAGRAM_TOKEN=...`, e habilite em `config.yaml`.

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

## Instalando o AWTRIX 3 no relógio

> Substitui o firmware da Ulanzi (e a tela de configuração dela). A opção "Awtrix Simulator" do firmware de fábrica é para o AWTRIX 2, que está descontinuado, e não é usada aqui.

1. Use um cabo USB-C **de dados** (cabo só de carga não funciona) e ligue o relógio direto no Mac.
2. No **Chrome ou Edge** (o Safari não tem Web Serial), abra o [flasher](https://blueforcer.github.io/awtrix3/#/flasher) → *Connect/Install* → escolha a porta `usbserial`/`wchusbserial`.
3. Depois de reiniciar, conecte-se à rede Wi-Fi `awtrix_xxxx` (senha `12345678`), configure o Wi-Fi de casa e anote o IP.
4. Se a porta não aparecer, rode `ls /dev/cu.*`. Quase sempre o problema é o cabo.
5. Na interface web do relógio, desligue os apps nativos que não quiser e ajuste o brilho.
6. Coloque `http://<ip>` em `targets` no `config.yaml`.
7. Rode `awtrix-hub icons`. Além dos IDs da LaMetric, a config aceita ícones próprios: um GIF 8×8 em `icons/` (ex.: `icons/gh_stars.gif`, o Octocat com estrela) referenciado pelo nome (`stars: "gh_stars"`). O AWTRIX não baixa ícones sozinho: eles precisam estar na pasta `/ICONS` do relógio, em GIF ou JPG. O comando baixa os ícones da LaMetric, converte de PNG para GIF quando precisa e envia. O `run` faz o mesmo ao iniciar.

## Rodando sempre (launchd)

```sh
make build
cp deploy/launchd/dev.omarco.awtrix-hub.plist ~/Library/LaunchAgents/
launchctl bootstrap gui/$(id -u) ~/Library/LaunchAgents/dev.omarco.awtrix-hub.plist
tail -f ~/Library/Logs/awtrix-hub.log
```

Quando o Mac dorme, as atualizações param. Os apps usam `lifetime`, então somem do relógio em vez de ficar mostrando dado velho.

## Ideias para depois

Clima e alerta de chuva · dólar/BTC · próxima reunião · CI e PRs · Spotify tocando · Pomodoro pelos botões · modo noturno · espectadores em live · migrar para um Raspberry Pi · MQTT/Home Assistant.
