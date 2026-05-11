PHP_ARG_ENABLE([herdlite],
  [whether to enable Herdlite support],
  [AS_HELP_STRING([--enable-herdlite], [Enable Herdlite extension])],
  [no])

if test "$PHP_HERDLITE" != "no"; then
  PHP_NEW_EXTENSION([herdlite], [herdlite.c], [$ext_shared])
fi

