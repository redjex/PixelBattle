type Props = { onBack: () => void };

const PRIVACY = [
  'Для входа Pixel Battle получает от Telegram идентификатор пользователя, имя, username и ссылку на аватар, если они указаны в профиле. Подписанные данные Telegram используются для проверки подлинности входа.',
  'Проект хранит игровой профиль, историю поставленных пикселей, их координаты, цвета и время установки, статистику, инвентарь и сведения о полученных наградах.',
  'Эти данные нужны для работы общей карты, отображения авторства пикселей, подсчёта статистики, уровней и наград, а также для применения игровых ограничений.',
  'Идентификатор, username и аватар автора могут быть показаны другим игрокам при просмотре закрашенного пикселя. Проект не использует эти данные для рекламной рассылки.',
  'Игровые данные хранятся в базе данных и Redis проекта. Автоматический срок удаления сейчас не установлен. Запросить удаление или уточнение данных можно у разработчика: @redjex.',
  'Изображение, выбранное как шаблон для рисования, обрабатывается локально в браузере и не отправляется на сервер Pixel Battle.',
];

const RULES = [
  'Запрещены боты, автокликеры, скрипты и другие средства автоматической установки пикселей.',
  'Нельзя использовать ошибки проекта, подменять запросы или обходить задержку, заморозку, авторизацию и другие серверные ограничения.',
  'Запрещено размещать незаконный контент, порнографию, экстремистскую символику, угрозы, призывы к насилию и разжигание ненависти.',
  'Нельзя публиковать чужие персональные данные, выдавать себя за другого человека, размещать спам и рекламу без согласования с администрацией.',
  'Перекрашивание чужих незамороженных пикселей является обычной частью Pixel Battle и само по себе не считается нарушением.',
  'Администрация может удалить запрещённый рисунок, приостановить игру или ограничить доступ пользователя, нарушающего эти правила.',
];

export function AgreementScreen({ onBack }: Props) {
  return (
    <div className="agreement-screen">
      <section className="agreement-content" aria-label="Политика конфиденциальности и правила Pixel Battle">
        <h1>Политика конфиденциальности</h1>
        <p className="agreement-intro">Какие данные использует Pixel Battle и зачем:</p>
        <ol className="agreement-rules">
          {PRIVACY.map((item) => <li key={item}>{item}</li>)}
        </ol>

        <h2>Правила Pixel Battle</h2>
        <ol className="agreement-rules">
          {RULES.map((rule) => <li key={rule}>{rule}</li>)}
        </ol>

        <p className="agreement-note">Продолжая пользоваться Pixel Battle, вы принимаете настоящую политику и обязуетесь соблюдать правила проекта. Последнее обновление: 4 сентября 2026 года.</p>

        <h2>Команда проекта:</h2>
        <div className="developer-list">
          <article className="developer-card">
            <img src="/assets/redjex.jpg" alt="redjex" />
            <div><a href="https://t.me/redjex" target="_blank" rel="noreferrer">redjex</a><p>Разработчик проекта</p></div>
          </article>
          <article className="developer-card">
            <img src="/assets/idea.png" alt="IdeaAnimator" />
            <div><a href="https://t.me/IdeaAnimator" target="_blank" rel="noreferrer">IdeaAnimator</a><p>Дизайнер и Идеолог проекта</p></div>
          </article>
          <article className="developer-card">
            <video src="/assets/soun.mp4" autoPlay loop muted playsInline aria-label="Арт от Oktokto" />
            <div><a href="https://t.me/ocktokto" target="_blank" rel="noreferrer">Oktokto</a><p>Художник арта «PixelBattle»</p></div>
          </article>
        </div>
      </section>
      <button className="stats-back" onClick={onBack}>Назад</button>
    </div>
  );
}
