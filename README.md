# Mister lister

[English version](README_En.md)

Списочный бот. Поддерживает работу с несколькими списками и совместную работу.

[@retsil_retsim_bot](https://t.me/retsil_retsim_bot)

![@retsil_retsim_bot](images/misterlister.jpg){width=360 height=238}

Автор бота не гарантирует сохранность и приватность ваших списков. Для гарантии запускайте бота у себя, пожалуйста.

Связаться с автором: [@uscr0](https://t.me/uscr0)

Оригинал этого репозитория: [gitlab.uscr.ru/mister-lister](https://gitlab.uscr.ru/public-projects/telegram-bots/mister-lister)

## Начало работы
 - Создать список: `/new Список покупок`
 - Переключаться между разными списками: команда `/list` или кнопка `Alt+Tab`

Для заполнения списка отправляйте новые пункты в чат как обычные сообщения.

Для удаления элемента из списка нажмите кнопку элемента.

Назначение кнопок нижнего ряда:

 - "F5" обновляет список (аналогично команде /show, актуально для совместных списков)
 - "Alt+Tab" переключает списки (аналогично команде /list)
 - "Ctrl+Z" отменяет удаление

## Совместная работа
 - Попросить пользователя узнать свой ID командой `/me`
 - Дать другому пользователю доступ к текущему списку: `/share <ID пользователя>`
 - Попросить пользователя выбрать список `/list`

Пользователь с которым вы поделились списком может расшарить список другому пользователю.

Пользователи могут добавлять и удалять элементы совместного списка без ограничений.

Каждый пользователь совместного списка может отменить удаление только тех элементов, которые добавил он сам.
