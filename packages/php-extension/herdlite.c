#ifdef HAVE_CONFIG_H
# include "config.h"
#endif

#include "php.h"
#include "ext/standard/info.h"
#include "php_herdlite.h"

ZEND_BEGIN_ARG_WITH_RETURN_TYPE_INFO_EX(arginfo_herdlite_loaded, 0, 0, _IS_BOOL, 0)
ZEND_END_ARG_INFO()

PHP_FUNCTION(herdlite_loaded)
{
	RETURN_TRUE;
}

static const zend_function_entry herdlite_functions[] = {
	PHP_FE(herdlite_loaded, arginfo_herdlite_loaded)
	PHP_FE_END
};

PHP_MINFO_FUNCTION(herdlite)
{
	php_info_print_table_start();
	php_info_print_table_header(2, "herdlite support", "enabled");
	php_info_print_table_row(2, "version", PHP_HERDLITE_VERSION);
	php_info_print_table_end();
}

zend_module_entry herdlite_module_entry = {
	STANDARD_MODULE_HEADER,
	"herdlite",
	herdlite_functions,
	NULL,
	NULL,
	NULL,
	NULL,
	PHP_MINFO(herdlite),
	PHP_HERDLITE_VERSION,
	STANDARD_MODULE_PROPERTIES
};

#ifdef COMPILE_DL_HERDLITE
# ifdef ZTS
ZEND_TSRMLS_CACHE_DEFINE()
# endif
ZEND_GET_MODULE(herdlite)
#endif

