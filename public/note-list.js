(() => {
  function elementFromHTML(html) {
    const template = document.createElement('template');
    template.innerHTML = html;
    return template.content.firstElementChild;
  }

  // Keep unchanged cards attached to preserve selection, editors and menus.
  // State snapshots contain primitive values, so in-place model changes are
  // detected without serializing note bodies or regenerating their HTML.
  function create(list) {
    const cardStates = new WeakMap(), headings = new WeakMap();
    return {
      render(groups, activeID = null) {
        const focused = list.contains(document.activeElement) ? document.activeElement : null;
        const inputSelection = focused && typeof focused.selectionStart === 'number'
          ? [focused.selectionStart, focused.selectionEnd, focused.selectionDirection] : null;
        const selection = window.getSelection();
        const selected = selection && list.contains(selection.anchorNode) && list.contains(selection.focusNode)
          ? [selection.anchorNode, selection.anchorOffset, selection.focusNode, selection.focusOffset] : null;
        const sections = new Map([...list.children].filter(node => node.matches('.date-group'))
          .map(node => [node.dataset.groupKey, node]));
        const cards = new Map([...list.querySelectorAll('.note-card')].map(card => [card.id, card]));
        const added = [], removed = [], retained = new Set(), retainedSections = new Set();
        const wanted = new Set(groups.flatMap(group => group.cards.map(item => `note-${item.id}`)));
        for (const [id, card] of cards) if (!wanted.has(id)) {
          card.remove();
          cards.delete(id);
          removed.push(card);
        }
        let sectionCursor = list.firstElementChild;
        for (const group of groups) {
          let section = sections.get(group.key);
          if (!section) {
            section = document.createElement('section');
            section.className = 'date-group';
            section.dataset.groupKey = group.key;
          }
          section.classList.toggle('pinned-section', group.pinned);
          section.classList.toggle('is-collapsed', group.collapsed);
          if (headings.get(section) !== group.headingHTML) {
            const heading = elementFromHTML(group.headingHTML);
            if (section.firstElementChild?.matches('h3')) section.firstElementChild.replaceWith(heading);
            else section.prepend(heading);
            headings.set(section, group.headingHTML);
            section.setAttribute('aria-labelledby', heading.id);
          }
          if (section !== sectionCursor) list.insertBefore(section, sectionCursor);
          sectionCursor = section.nextElementSibling;
          retainedSections.add(section);
          let cardCursor = section.firstElementChild.nextElementSibling;
          for (const item of group.cards) {
            const id = `note-${item.id}`;
            let card = cards.get(id);
            const previous = card && cardStates.get(card);
            const unchanged = previous && previous.length === item.state.length &&
              previous.every((value, index) => value === item.state[index]);
            if (!card || (!unchanged && item.id !== activeID)) {
              const next = elementFromHTML(item.html());
              cardStates.set(next, item.state);
              if (card) {
                if (cardCursor === card) cardCursor = next;
                card.replaceWith(next);
                removed.push(card);
                cards.delete(id);
              }
              card = next;
              added.push(card);
            }
            if (card !== cardCursor) section.insertBefore(card, cardCursor);
            cardCursor = card.nextElementSibling;
            retained.add(card);
          }
        }
        for (const card of cards.values()) {
          if (!retained.has(card)) {
            card.remove();
            removed.push(card);
          }
        }
        for (const node of [...list.children]) if (!retainedSections.has(node)) node.remove();
        // Moving a card between date groups can blur an attached editor. Only
        // restore focus/selection for nodes that survived reconciliation.
        if (focused?.isConnected && document.activeElement !== focused) {
          focused.focus({ preventScroll: true });
          if (inputSelection) focused.setSelectionRange(...inputSelection);
        }
        if (selected?.[0].isConnected && selected[2].isConnected &&
            (selection.anchorNode !== selected[0] || selection.anchorOffset !== selected[1] ||
             selection.focusNode !== selected[2] || selection.focusOffset !== selected[3])) {
          selection.setBaseAndExtent(...selected);
        }
        return { added, removed };
      },
    };
  }

  window.GnotesNoteList = { create };
})();
