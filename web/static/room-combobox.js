/* The Room field on the plant form. The server renders a text input and a
   <datalist> of the rooms the garden's plants are in. This script takes the
   list attribute off the input and shows those rooms in a listbox of its own
   instead. The field posts the same value under the same name with or without
   this script. */
(function () {
  const input = document.getElementById('room');
  const list = document.getElementById('room-list');
  const datalist = document.getElementById('rooms');
  // count is the live region under the field. It says how many rooms the
  // listbox is showing, because a screen reader is not told when the list
  // changes.
  const count = document.getElementById('room-count');
  if (!input || !list || !datalist || !count) return;

  const rooms = Array.from(datalist.options, (option) => option.value);

  // The datalist is there for a browser running no script. Removing the list
  // attribute stops the browser opening its own dropdown over the listbox.
  input.removeAttribute('list');
  input.setAttribute('role', 'combobox');
  input.setAttribute('aria-controls', 'room-list');
  input.setAttribute('aria-autocomplete', 'list');
  input.setAttribute('aria-expanded', 'false');

  // options is what the listbox is showing. active is the index of the option
  // the arrow keys have highlighted, or -1 when none is highlighted.
  let options = [];
  let active = -1;

  // canonical returns the garden's spelling of the room typed, or undefined
  // when the garden has no room by that name.
  const canonical = (typed) => rooms.find((room) => room.toLowerCase() === typed.trim().toLowerCase());

  const close = () => {
    list.hidden = true;
    input.setAttribute('aria-expanded', 'false');
    input.removeAttribute('aria-activedescendant');
    count.textContent = '';
    active = -1;
  };

  // counted is the sentence for the options shown: how many of the garden's
  // rooms match, and whether the last option offers to add a new one.
  const counted = () => {
    const matches = options.filter((option) => !option.fresh).length;
    const rooms = matches === 1 ? '1 room' : matches + ' rooms';
    const fresh = options.find((option) => option.fresh);
    if (!fresh) return rooms;
    if (matches === 0) return 'No room called “' + fresh.room + '”. Choose the option to add it.';
    return rooms + ', or add “' + fresh.room + '” as a new room';
  };

  // showOptions fills the listbox with the rooms containing what has been
  // typed, then the typed text itself as a new room when no room has that name.
  // That last option quotes the text so a typo can be read before it is saved.
  const showOptions = () => {
    const typed = input.value.trim();
    options = rooms
      .filter((room) => room.toLowerCase().includes(typed.toLowerCase()))
      .map((room) => ({ room, fresh: false }));
    if (typed && !canonical(typed)) options.push({ room: typed, fresh: true });
    if (!options.length) return close();

    list.replaceChildren(
      ...options.map((option, i) => {
        const li = document.createElement('li');
        li.className = option.fresh ? 'combo__option combo__option--new' : 'combo__option';
        li.id = `room-opt-${i}`;
        li.setAttribute('role', 'option');
        li.setAttribute('aria-selected', String(i === active));
        li.dataset.index = String(i);
        li.textContent = option.fresh ? `Add “${option.room}” as a new room` : option.room;
        return li;
      }),
    );
    list.hidden = false;
    input.setAttribute('aria-expanded', 'true');
    if (active < 0) {
      input.removeAttribute('aria-activedescendant');
    } else {
      input.setAttribute('aria-activedescendant', `room-opt-${active}`);
    }
    count.textContent = counted();
  };

  const choose = (i) => {
    if (!options[i]) return;
    input.value = options[i].room;
    close();
  };

  input.addEventListener('focus', showOptions);
  input.addEventListener('input', () => {
    active = -1;
    showOptions();
  });

  /* Leaving the field sets it to the garden's spelling of the room typed, so
     "bathroom" becomes Bathroom rather than a second room beside it on the
     Plants list. The server applies the same rule to what is posted, so a
     browser running no script stores the same value. */
  input.addEventListener('blur', () => {
    const match = canonical(input.value);
    if (match) input.value = match;
    close();
  });

  // Escape closes the list and leaves the text typed. Enter with an option
  // highlighted takes it. Home and End go to the first and last option.
  input.addEventListener('keydown', (event) => {
    if (event.key === 'Escape') {
      if (!list.hidden) event.preventDefault();
      return close();
    }
    if ((event.key === 'Home' || event.key === 'End') && !list.hidden) {
      event.preventDefault();
      active = event.key === 'Home' ? 0 : options.length - 1;
      return showOptions();
    }
    if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
      event.preventDefault();
      if (list.hidden) return showOptions();
      // With no option highlighted, Down takes the first and Up the last.
      if (active < 0) active = event.key === 'ArrowDown' ? 0 : options.length - 1;
      else active = (active + (event.key === 'ArrowDown' ? 1 : options.length - 1)) % options.length;
      return showOptions();
    }
    // Enter still submits the form when no option is highlighted, because that
    // is what Enter does in the other fields on this page.
    if (event.key === 'Enter' && !list.hidden && active >= 0) {
      event.preventDefault();
      choose(active);
    }
  });

  // mousedown rather than click, because preventDefault on it keeps the focus
  // in the input. A click would land after the field had lost focus.
  list.addEventListener('mousedown', (event) => {
    const option = event.target.closest('[data-index]');
    if (!option) return;
    event.preventDefault();
    choose(Number(option.dataset.index));
  });
})();
