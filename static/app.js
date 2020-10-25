window.onload = function() {
  let media = window.matchMedia('(prefers-color-scheme: dark)');
  let theme = "github-light";
  if (media.matches) {
    theme = "github-dark";
  }

  const sql = document.getElementById('sql');
  let sqlEditor = CodeMirror.fromTextArea(sql, {
    mode: 'text/x-pgsql',
    indentWithTabs: true,
    smartIndent: true,
    lineNumbers: true,
    matchBrackets : true,
    autofocus: true, 
    theme: theme,
    // viewportMargin: Infinity,
  });
  sql.classList.remove("hidden");

  const go = document.getElementById('go');
  let codeView = CodeMirror.fromTextArea(go, {
    mode: 'text/x-go',
    indentWithTabs: true,
    smartIndent: true,
    lineNumbers: true,
    matchBrackets : true,
    readOnly: true,
    theme: theme,
    // viewportMargin: Infinity,
  });
  go.classList.remove("hidden");

  let godoc = document.getElementById('godoc');
  let errors = document.getElementById('errors');
  let stderr = document.getElementById('stderr');

  let sqlDoc, cfgDoc;

  let loadInput = function(data) {
    // Remove the existing tabs
    let tabs = document.getElementById('sqlt');
    while (tabs.firstChild) {
      tabs.firstChild.remove();
    }

    // TODO: Don't remove the first child of the tabs
    let li = document.createElement("li");
    let span = document.createElement("span");
    span.innerText = 'Input';
    li.appendChild(span);
    tabs.appendChild(li);

    // Create documents for each
    for (let i = 0; i < data.files.length; i++) {
      const file = data.files[i];
      const doc = CodeMirror.Doc(file.contents, file.contentType);

      if (file.name === 'query.sql') {
        sqlDoc = doc
      }
      if (file.name === 'sqlc.json' || file.name === 'sqlc.json') {
        cfgDoc = doc
      }

      // Create a new tab for each document
      const a = document.createElement("a");
      a.innerText = file.name;
      a.href = "#input=" + file.name;
      a.onclick = function(e) {
        e.preventDefault();
        
        if (a.classList.contains("selected")) {
          return;
        }
        
        // Set contents of the editor to the selected document
        sqlEditor.swapDoc(doc);

        // Unset the current selected tab
        var selected = document.querySelector("#sqlt li a.selected");
        if (selected) {
          selected.classList.remove("selected");
        }

        // Set this tab as the selected one
        a.classList.add("selected");
      };

      if (file.name.endsWith(".sql")) {
        a.classList.add("selected");
        sqlEditor.swapDoc(doc);
      }

      const li = document.createElement("li");
      li.appendChild(a);
      tabs.appendChild(li);
    }

    godoc.classList.remove('hidden');
    errors.classList.add("hidden");
  };


  let loadOutput = function(data) {
    if (data.sha) {
      history.replaceState(data, 'sqlc playground', "/p/"+data.sha)
    }

    if (data.errored) {
      // GoDoc pane
      godoc.classList.add('hidden');
      // Error pane
      errors.classList.remove("hidden");
      stderr.innerText = data.error || "500: Internal Server Error";
    } else {
      // Remove the existing tabs
      let tabs = document.getElementById('codet');
      while (tabs.firstChild) {
        tabs.firstChild.remove();
      }

      // TODO: Don't remove the first child of the tabs
      let li = document.createElement("li");
      let span = document.createElement("span");
      span.innerText = 'Output';
      li.appendChild(span);
      tabs.appendChild(li);

      // Create documents for each
      for (let i = 0; i < data.files.length; i++) {
        const file = data.files[i];
        const doc = CodeMirror.Doc(file.contents, file.contentType);

        // Create a new tab for each document
        const a = document.createElement("a");
        a.innerText = file.name;
        a.href = "#output=" + file.name;
        a.onclick = function(e) {
          e.preventDefault();
          
          if (a.classList.contains("selected")) {
            return;
          }
          
          // Set contents of the editor to the selected document
          codeView.swapDoc(doc);

          // Unset the current selected tab
          var selected = document.querySelector("#codet li a.selected");
          if (selected) {
            selected.classList.remove("selected");
          }

          // Set this tab as the selected one
          a.classList.add("selected");
        };

        if (file.name.endsWith(".sql.go")) {
          a.classList.add("selected");
          codeView.swapDoc(doc);
        }

        const li = document.createElement("li");
        li.appendChild(a);
        tabs.appendChild(li);
      }

      godoc.classList.remove('hidden');
      errors.classList.add("hidden");
    }
  };

  let lastTimeout = 0;

  sqlEditor.on("change", function(instance, change) {
    if (lastTimeout > 0) {
        clearTimeout(lastTimeout);
    }
    if (!sqlDoc || !cfgDoc) {
      return
    }
    lastTimeout = setTimeout(function() {
      fetch('/generate', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
        },
        body: JSON.stringify({
          query: sqlDoc.getValue(),
          config: cfgDoc.getValue(),
        }),
      })
      .then((response) => {
        return response.json();
      })
      .then((data) => {
        loadOutput(data);
      })
      .catch((error) => {
        console.error('Error:', error);
      });
    }, 500);
  });

  let input = document.getElementById('input');
  if (input) {
    loadInput(JSON.parse(input.innerText));
  }

  let output = document.getElementById('output');
  if (output) {
    loadOutput(JSON.parse(output.innerText));
  }
};
