const $ = (id) => document.getElementById(id);
const samples = { A: {bid:150,ctr:0.05}, B: {bid:100,ctr:0.10}, C: {bid:180,ctr:0.03} };
const logEntries = [];
const format = (num) => Number(num).toLocaleString('ru-RU', {maximumFractionDigits: 6});

function updateEffective() {
  const bid = Number($('bid').value);
  const ctr = Number($('ctr').value);
  $('effective').textContent = Number.isFinite(bid * ctr) ? format(bid * ctr) : '—';
}

async function requestJSON(path, options) {
  const response = await fetch(path, {cache:'no-store', ...options});
  let data;
  try { data = await response.json(); } catch { throw new Error(`Ответ сервера не JSON (HTTP ${response.status})`); }
  if (!response.ok) throw new Error(data.error || `HTTP ${response.status}`);
  return data;
}

function addLog(message, ok) {
  logEntries.unshift({message, ok, time:new Date().toLocaleTimeString('ru-RU')});
  if (logEntries.length > 12) logEntries.pop();
  const list = $('activity');
  list.replaceChildren();
  for (const entry of logEntries) {
    const item = document.createElement('div');
    item.className = `activity-item${entry.ok ? '' : ' fail'}`;
    const detail = document.createElement('div');
    const title = document.createElement('strong');
    title.textContent = entry.ok ? '✓ Ставка принята' : '✕ Ошибка отправки';
    const desc = document.createElement('small');
    desc.textContent = entry.message;
    detail.append(title, desc);
    const time = document.createElement('time');
    time.textContent = entry.time;
    item.append(detail, time);
    list.append(item);
  }
}

function stat(label, value, extraClass = '') {
  const block = document.createElement('div');
  block.className = `mini-stat ${extraClass}`;
  const caption = document.createElement('span');
  caption.textContent = label;
  const number = document.createElement('b');
  number.textContent = format(value);
  block.append(caption, number);
  return block;
}

function renderWinner(data) {
  const root = $('winner-content');
  root.className = '';
  root.replaceChildren();
  const name = document.createElement('div');
  name.className = 'winner-name';
  const trophy = document.createElement('div'); trophy.className = 'trophy'; trophy.textContent = '🏆';
  const info = document.createElement('div');
  const small = document.createElement('small'); small.textContent = 'Победил рекламодатель';
  const title = document.createElement('strong'); title.textContent = data.advertiser_id;
  info.append(small, title); name.append(trophy, info);
  const grid = document.createElement('div'); grid.className = 'winner-grid';
  grid.append(stat('Исходная ставка', data.bid), stat('CTR', data.predicted_ctr), stat('Эффективная ставка', data.effective_bid), stat('Цена к оплате', data.price, 'price'));
  const mode = document.createElement('div'); mode.className = 'mode';
  mode.textContent = `Тип аукциона: ${data.auction_type}`;
  root.append(name, grid, mode);
}

function renderStats(data) {
  $('total-bids').textContent = format(data.total_bids || 0);
  const entries = Object.entries(data.wins_by_advertiser || {}).map(([id, count]) => [id, Number(count)]).sort((a,b) => b[1]-a[1]);
  const root = $('wins'); root.replaceChildren();
  if (!entries.length) {root.className = 'empty compact'; root.textContent = 'Пока нет данных'; return;}
  root.className = '';
  const max = Math.max(...entries.map(x => x[1]), 1);
  for (const [id, count] of entries) {
    const row = document.createElement('div'); row.className = 'win-row';
    const label = document.createElement('span'); label.textContent = id; label.title = id;
    const track = document.createElement('div'); track.className = 'win-track';
    const fill = document.createElement('div'); fill.className = 'win-fill'; fill.style.width = `${100 * count / max}%`;
    track.append(fill);
    const number = document.createElement('b'); number.textContent = format(count);
    row.append(label,track,number); root.append(row);
  }
}

async function refresh() {
  const button = $('refresh'); button.disabled = true;
  const [winner, stats] = await Promise.allSettled([requestJSON('/winner'),requestJSON('/stats')]);
  if (winner.status === 'fulfilled') renderWinner(winner.value);
  else if (winner.reason.message === 'no bids yet') {
    $('winner-content').className = 'empty'; $('winner-content').textContent = 'Победитель появится после первой ставки.';
  } else {
    $('winner-content').className = 'empty'; $('winner-content').textContent = `Ошибка: ${winner.reason.message}`;
  }
  if (stats.status === 'fulfilled') renderStats(stats.value);
  const connected = stats.status === 'fulfilled';
  $('status').className = `status ${connected ? 'online' : 'offline'}`;
  $('status-text').textContent = connected ? 'Сервер подключён' : 'Нет соединения с API';
  button.disabled = false;
}

$('bid-form').addEventListener('submit', async (event) => {
  event.preventDefault();
  const advertiser_id = $('advertiser').value.trim();
  const bid = Number($('bid').value);
  const predicted_ctr = Number($('ctr').value);
  if (!advertiser_id || !Number.isFinite(bid) || bid <= 0 || !Number.isFinite(predicted_ctr) || predicted_ctr <= 0 || predicted_ctr > 1) {
    $('feedback').className = 'feedback error';
    $('feedback').textContent = 'Проверь поля: ставка > 0, CTR от 0 (не включительно) до 1.';
    return;
  }
  $('send').disabled = true;
  try {
    const winner = await requestJSON('/bid', {method:'POST', headers:{'Content-Type':'application/json'}, body:JSON.stringify({advertiser_id,bid,predicted_ctr})});
    $('feedback').className = 'feedback success';
    $('feedback').textContent = `Ставка принята! Текущий победитель: ${winner.advertiser_id}.`;
    addLog(`${advertiser_id} · ставка ${format(bid)} · CTR ${format(predicted_ctr)}`, true);
    renderWinner(winner);
    await refresh();
    // Consumer обновляет статистику асинхронно: повторяем чтение через короткий интервал.
    setTimeout(refresh, 450);
  } catch (error) {
    $('feedback').className = 'feedback error';
    $('feedback').textContent = error.message;
    addLog(error.message, false);
    await refresh();
  } finally { $('send').disabled = false; }
});

for (const input of ['bid','ctr']) $(input).addEventListener('input',updateEffective);
for (const button of document.querySelectorAll('[data-sample]')) {
  button.addEventListener('click', () => {
    const id = button.dataset.sample;
    $('advertiser').value = id;
    $('bid').value = samples[id].bid;
    $('ctr').value = samples[id].ctr;
    updateEffective();
  });
}
$('refresh').addEventListener('click',refresh);
$('clear-log').addEventListener('click', () => {logEntries.length = 0; $('activity').innerHTML = '<div class="empty compact">Журнал очищен.</div>';});
updateEffective();
refresh();
