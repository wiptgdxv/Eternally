(() => {
  const demo = document.querySelector('[data-demo]');
  if (!demo) return;

  const phrases = {
    nihao: [
      { text: '你好', en: 'Hello', ja: 'こんにちは', fr: 'Bonjour' },
      { text: '拟好', en: 'Drafted', ja: '下書き済み', fr: 'Rédigé' },
      { text: '你号', en: 'Your number', ja: 'あなたの番号', fr: 'Votre numéro' },
    ],
    xiexie: [
      { text: '谢谢', en: 'Thank you', ja: 'ありがとう', fr: 'Merci' },
      { text: '写写', en: 'Write a little', ja: '少し書く', fr: 'Écrire un peu' },
    ],
    zaoshanghao: [
      { text: '早上好', en: 'Good morning', ja: 'おはようございます', fr: 'Bonjour' },
    ],
    huiyigaidaosandian: [
      {
        text: '会议改到三点',
        en: 'The meeting has been moved to 3 p.m.',
        ja: '会議は午後3時に変更になりました。',
        fr: 'La réunion est reportée à 15 heures.',
      },
    ],
    xiexienidebangzhu: [
      {
        text: '谢谢你的帮助',
        en: 'Thank you for your help.',
        ja: 'ご協力ありがとうございます。',
        fr: 'Merci pour votre aide.',
      },
    ],
    yonghuxinxi: [
      { text: '用户信息', en: 'User information', ja: 'ユーザー情報', fr: 'Informations utilisateur' },
    ],
  };
  const scenarios = {
    mail: { title: '读懂对方的新安排', phrase: 'huiyigaidaosandian', source: 'en', target: 'zh' },
    chat: { title: '用日语，把谢意说清楚', phrase: 'xiexienidebangzhu', source: 'zh', target: 'ja' },
    code: { title: '为代码找到合适的表达', phrase: 'yonghuxinxi', source: 'zh', target: 'en' },
  };
  const languages = { zh: '中文', en: '英语', ja: '日语', fr: '法语' };
  const languageKeys = Object.keys(languages);
  const entries = Object.entries(phrases).flatMap(([key, values]) => values.map(value => ({
    ...value, zh: value.text, pinyin: key,
  })));
  const pinyin = demo.querySelector('#pinyin');
  const candidates = demo.querySelector('[data-candidates]');
  const draftText = demo.querySelector('[data-draft]');
  const preview = demo.querySelector('[data-preview]');
  const output = demo.querySelector('#output');
  const advance = demo.querySelector('[data-advance]');
  const status = demo.querySelector('[data-demo-status]');
  const sourceSelect = demo.querySelector('#source-language');
  const targetSelect = demo.querySelector('#target-language');
  const exampleButtons = demo.querySelectorAll('[data-example]');
  const scenarioButtons = document.querySelectorAll('button[data-scenario]');
  const scenarioPreview = document.querySelector('[data-scenario-preview]');
  const pointer = window.matchMedia('(pointer: fine)');

  let source = 'zh';
  let target = 'en';
  let highlighted = 0;
  let choices = [];
  let draft = null;
  let submitted = false;

  function textOf(entry, language) {
    return language === 'zh' ? entry.text : entry[language];
  }

  function normalized(value) {
    return value.trim().toLocaleLowerCase().replace(/[.!?。！？]+$/u, '').replace(/\s+/g, ' ');
  }

  function rawEntry(value) {
    if (!value.trim()) return null;
    return entries.find(entry => normalized(textOf(entry, source)) === normalized(value)) || null;
  }

  function selectedEntry() {
    return draft || (source === 'zh' ? choices[highlighted] : rawEntry(pinyin.value));
  }

  function currentPinyin() {
    return pinyin.value.toLowerCase().replace(/[\s’']/g, '');
  }

  function findChoices(input) {
    if (!input) return [];
    if (Object.hasOwn(phrases, input)) return entries.filter(choice => choice.pinyin === input);
    const chineseMatches = entries.filter(choice => choice.text === input);
    if (chineseMatches.length) return chineseMatches;
    return Object.entries(phrases)
      .filter(([key]) => key.startsWith(input))
      .map(([key]) => entries.find(choice => choice.pinyin === key));
  }

  function focusPinyin(select = false) {
    if (!pointer.matches) return;
    pinyin.focus({ preventScroll: true });
    if (select) pinyin.select();
  }

  function showSelection() {
    [...candidates.children].forEach((button, index) => {
      if (!button.matches('button')) return;
      const selected = String(index === highlighted);
      button.dataset.selected = selected;
      button.setAttribute('aria-pressed', selected);
    });
  }

  function drawCandidates() {
    candidates.replaceChildren();
    if (!choices.length) {
      const empty = document.createElement('span');
      empty.className = 'demo-empty';
      if (draft || (source !== 'zh' && rawEntry(pinyin.value))) {
        empty.textContent = `回车提交${languages[target]}译文`;
      } else {
        empty.textContent = pinyin.value ? '演示仅支持下方内置例句' : source === 'zh' ? '等待拼音' : '直接输入原文';
      }
      candidates.append(empty);
      return;
    }

    choices.forEach((choice, index) => {
      const button = document.createElement('button');
      button.type = 'button';
      button.className = 'candidate';
      const number = document.createElement('kbd');
      number.textContent = index + 1;
      const word = document.createElement('span');
      word.textContent = choice.text;
      button.append(number, word);
      button.addEventListener('click', () => {
        choose(index);
        focusPinyin();
      });
      candidates.append(button);
    });
    showSelection();
  }

  function drawProgress() {
    const entry = draft || (source !== 'zh' ? rawEntry(pinyin.value) : null);
    demo.dataset.stage = entry ? 'preview' : submitted ? 'submitted' : 'compose';
    demo.dataset.source = source;
    draftText.textContent = draft?.text ?? '';
    preview.textContent = entry ? textOf(entry, target) : source === 'zh' ? '选词后显示译文' : '输入内置例句，预览译文';
    preview.lang = entry ? target : 'zh-CN';
    advance.disabled = !entry && !choices.length;

    if (draft) {
      status.textContent = `再按回车，提交${languages[target]}译文。`;
    } else if (entry) {
      status.textContent = `按一次回车，提交${languages[target]}译文。`;
    } else if (submitted) {
      status.textContent = '译文已写入输入框，可以继续体验。';
    } else if (choices.length) {
      status.textContent = `按回车选中「${choices[highlighted].text}」，再按一次上屏。`;
    } else {
      status.textContent = source === 'zh' ? '试试 nihao、xiexie 或 zaoshanghao。' : `试试下方的${languages[source]}例句。`;
    }
  }

  function showPinyin(value) {
    pinyin.value = value;
    draft = null;
    submitted = false;
    highlighted = 0;
    choices = source === 'zh' ? findChoices(currentPinyin()) : [];
    drawCandidates();
    drawProgress();
  }

  function choose(index) {
    const candidate = choices[index];
    if (!candidate) return;
    draft = candidate;
    pinyin.value = '';
    choices = [];
    drawCandidates();
    drawProgress();
  }

  function submitOrChoose() {
    if (source === 'zh' && !draft) {
      choose(highlighted);
      return;
    }

    const entry = selectedEntry();
    if (!entry) return;
    const translation = textOf(entry, target);
    output.value = output.value ? `${output.value}\n${translation}` : translation;
    output.lang = target;
    output.scrollTop = output.scrollHeight;
    pinyin.value = '';
    draft = null;
    submitted = true;
    drawCandidates();
    drawProgress();
  }

  function updatePairControls() {
    sourceSelect.value = source;
    targetSelect.value = target;
    pinyin.lang = source;
    pinyin.placeholder = source === 'zh' ? '输入拼音，例如 nihao…' : `输入${languages[source]}，例如 ${textOf(entries[0], source)}…`;
    exampleButtons.forEach(button => {
      button.textContent = textOf(phrases[button.dataset.example][0], source);
    });
  }

  function setPair(nextSource, nextTarget) {
    if (!Object.hasOwn(languages, nextSource) || !Object.hasOwn(languages, nextTarget) || nextSource === nextTarget) return;
    const entry = selectedEntry() || entries[0];
    const sourceChanged = source !== nextSource;
    source = nextSource;
    target = nextTarget;
    updatePairControls();
    if (sourceChanged) {
      showPinyin(source === 'zh' ? entry.pinyin : textOf(entry, source));
      return;
    }
    drawCandidates();
    drawProgress();
  }

  function reset() {
    output.value = '';
    showPinyin(source === 'zh' ? 'nihao' : textOf(entries[0], source));
  }

  function setScenario(name) {
    const scenario = scenarios[name];
    if (!scenario) return;
    const phrase = phrases[scenario.phrase][0];
    scenarioButtons.forEach(button => {
      button.setAttribute('aria-pressed', String(button.dataset.scenario === name));
    });
    if (scenarioPreview) scenarioPreview.dataset.scenario = name;
    document.querySelectorAll('[data-scenario-title]').forEach(node => {
      node.textContent = scenario.title;
    });
    document.querySelectorAll('[data-scenario-source]').forEach(node => {
      node.textContent = textOf(phrase, scenario.source);
      node.lang = scenario.source;
    });
    document.querySelectorAll('[data-scenario-result]').forEach(node => {
      node.textContent = textOf(phrase, scenario.target);
      node.lang = scenario.target;
    });
    document.querySelectorAll('[data-scenario-direction]').forEach(node => {
      node.textContent = `${languages[scenario.source]} → ${languages[scenario.target]}`;
    });
  }

  pinyin.addEventListener('input', event => {
    if (!event.isComposing) showPinyin(pinyin.value);
  });
  pinyin.addEventListener('compositionend', () => showPinyin(pinyin.value));
  pinyin.addEventListener('keydown', (event) => {
    if (event.isComposing || event.ctrlKey || event.altKey || event.metaKey) return;

    if (event.key === 'Enter') {
      event.preventDefault();
      if (!event.repeat) submitOrChoose();
    } else if (event.key === 'Escape') {
      event.preventDefault();
      showPinyin('');
    } else if (choices.length && (event.key === 'ArrowLeft' || event.key === 'ArrowRight')) {
      event.preventDefault();
      const direction = event.key === 'ArrowLeft' ? -1 : 1;
      highlighted = (highlighted + direction + choices.length) % choices.length;
      showSelection();
      drawProgress();
    } else if (choices.length && event.key === ' ') {
      event.preventDefault();
      if (!event.repeat) choose(highlighted);
    } else if (choices.length && /^[1-3]$/.test(event.key)) {
      event.preventDefault();
      if (!event.repeat) choose(Number(event.key) - 1);
    }
  });

  demo.addEventListener('keydown', (event) => {
    if (event.isComposing || event.key !== 'F9' || !event.ctrlKey || event.altKey || event.metaKey) return;
    event.preventDefault();
    if (!event.repeat) {
      const targets = languageKeys.filter(key => key !== source);
      setPair(source, targets[(targets.indexOf(target) + 1) % targets.length]);
    }
  });

  sourceSelect.addEventListener('change', () => {
    setPair(sourceSelect.value, sourceSelect.value === target ? source : target);
  });
  targetSelect.addEventListener('change', () => {
    setPair(targetSelect.value === source ? target : source, targetSelect.value);
  });
  demo.querySelector('[data-swap]').addEventListener('click', () => {
    setPair(target, source);
  });
  exampleButtons.forEach((button) => {
    button.addEventListener('click', () => {
      const entry = phrases[button.dataset.example][0];
      showPinyin(source === 'zh' ? button.dataset.example : textOf(entry, source));
      focusPinyin();
    });
  });
  scenarioButtons.forEach(button => {
    button.addEventListener('click', () => setScenario(button.dataset.scenario));
  });
  document.querySelectorAll('a[href="#pinyin"]').forEach(link => {
    link.addEventListener('click', event => {
      if (event.ctrlKey || event.metaKey || event.shiftKey || event.altKey) return;
      event.preventDefault();
      const reducedMotion = window.matchMedia('(prefers-reduced-motion: reduce)').matches;
      demo.scrollIntoView({ block: 'center', behavior: reducedMotion ? 'instant' : 'smooth' });
      focusPinyin(true);
    });
  });
  advance.addEventListener('click', () => {
    submitOrChoose();
    focusPinyin();
  });
  demo.querySelector('[data-reset]').addEventListener('click', () => {
    reset();
    focusPinyin();
  });

  showPinyin(pinyin.value || 'nihao');
  updatePairControls();
  const initialScenario = [...scenarioButtons].find(button => button.getAttribute('aria-pressed') === 'true');
  if (scenarioButtons.length) setScenario(initialScenario?.dataset.scenario || 'mail');
})();
