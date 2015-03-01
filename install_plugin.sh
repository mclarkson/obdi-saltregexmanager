#!/bin/bash
#
# Obdi - a REST interface and GUI for deploying software
# Copyright (C) 2014  Mark Clarkson
#
# This program is free software: you can redistribute it and/or modify
# it under the terms of the GNU General Public License as published by
# the Free Software Foundation, either version 3 of the License, or
# (at your option) any later version.
#
# This program is distributed in the hope that it will be useful,
# but WITHOUT ANY WARRANTY; without even the implied warranty of
# MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
# GNU General Public License for more details.
#
# You should have received a copy of the GNU General Public License
# along with this program.  If not, see <http://www.gnu.org/licenses/>.
#
# saltregexmanager plugin

[[ -z $guid ]] && {
  echo "ERROR: environment variable 'guid' must be set."
  exit 1
}

##
## TODO: This plugin depends on salt
##

proto="https"
opts="-k -s" # don't check ssl cert, silent
ipport="127.0.0.1:443"

#
# Create a temporary file and a trap to delete it
#

t="/tmp/install_saltregexmanager_$$"
touch $t
[[ $? -ne 0 ]] && {
    echo "Could not create temporary file. Aborting."
    exit 1
}
trap "rm -f -- '$t'" EXIT

#
# Create the plugin entry in obdi, so it can be shown in the sidebar
#

curl -k -d '{
    "Name":"saltregexmanager",
    "Desc":"Salt regex management plugin",
    "HasView":1,
    "Parent":"salt"
}' $proto://$ipport/api/admin/$guid/plugins | tee $t

# Grab the id of the last insert
id=`grep Id $t | grep -Eo "[0-9]+"`

#
# Add the AJS controller files
#
# These need to be loaded when the application starts
#

curl -k -d '{
    "Name":"saltregexmanager.js",
    "Desc":"Controller for Salt regex manager",
    "Type":1,
    "PluginId":'"$id"',
    "Url":"saltregexmanager/js/controllers/saltregexmanager.js"
}' $proto://$ipport/api/admin/$guid/files

#
# Add the scripts, removing comment lines (#) and empty lines
#

# source=`sed '1n;/^\s*#/d;/^$/d;' scripts/saltregex-showregex.sh | base64 -w 0`
# 
# curl -k -d '{
#     "Desc": "Return the list of regexes. No Args.",
#     "Name": "saltregex-showregex.sh",
#     "Source": "'"$source"'"
# }' $proto://$ipport/api/admin/$guid/scripts

# --

# Delete the temporary file and delete the trap
rm -f -- "$t"
trap - EXIT

