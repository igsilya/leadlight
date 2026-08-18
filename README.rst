Leadlight
=========

A terminal UI for `Patchwork <https://github.com/getpatchwork/patchwork>`_
to review the status of patch series, delegate them, change their state,
and apply them with ``git am``.

Configuration
-------------

Leadlight reads configuration from gitconfig.  Run it from within the
git repository of the project you want to track.

If `git-pw <https://github.com/getpatchwork/git-pw>`_ is already
configured (``pw.server``, ``pw.project``), leadlight picks up those
settings automatically.

Otherwise, use the ``leadlight.*`` namespace::

  git config leadlight.server https://patchwork.example.com/api/1.3
  git config leadlight.project your-project

  # Optional: API token for write access (state/delegate changes)
  git config leadlight.token YOUR_TOKEN

  # Required for API 1.2 instances (which lack comment events)
  git config leadlight.mailarchive https://lists.example.com/pipermail/dev/

Examples
~~~~~~~~

Open vSwitch::

  git config leadlight.server https://patchwork.ozlabs.org/api/1.2
  git config leadlight.project openvswitch
  git config leadlight.mailarchive https://mail.openvswitch.org/pipermail/ovs-dev/

OVN::

  git config leadlight.server https://patchwork.ozlabs.org/api/1.2
  git config leadlight.project ovn
  git config leadlight.mailarchive https://mail.openvswitch.org/pipermail/ovs-dev/

DPDK::

  git config leadlight.server https://patches.dpdk.org/api/1.3
  git config leadlight.project dpdk

Linux kernel netdev::

  git config leadlight.server https://patchwork.kernel.org/api/1.3
  git config leadlight.project netdevbpf

Building
--------

Requires Go 1.24 or later and a C compiler (gcc or clang) for the
SQLite driver.

::

  make build

To install into ``$GOBIN`` (default ``~/go/bin``)::

  make install

Then run ``leadlight`` from the git repository of the project you
want to track.

License
-------

Apache-2.0.  See `LICENSE <LICENSE>`_ and `AUTHORS <AUTHORS>`_.
